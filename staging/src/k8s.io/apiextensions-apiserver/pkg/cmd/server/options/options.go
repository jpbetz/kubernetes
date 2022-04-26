/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package options

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/gogo/protobuf/proto"
	cel "github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types/ref"
	"github.com/spf13/pflag"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	netutils "k8s.io/utils/net"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	schemacel "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/library"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	celmodel "k8s.io/apiextensions-apiserver/third_party/forked/celopenapi/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/initializer"
	celhandlers "k8s.io/apiserver/pkg/endpoints/handlers/cel"
	genericregistry "k8s.io/apiserver/pkg/registry/generic"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/util/proxy"
	"k8s.io/apiserver/pkg/util/webhook"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const defaultEtcdPathPrefix = "/registry/apiextensions.kubernetes.io"

// CustomResourceDefinitionsServerOptions describes the runtime options of an apiextensions-apiserver.
type CustomResourceDefinitionsServerOptions struct {
	ServerRunOptions   *options.ServerRunOptions
	RecommendedOptions *genericoptions.RecommendedOptions
	APIEnablement      *genericoptions.APIEnablementOptions

	StdOut io.Writer
	StdErr io.Writer
}

// NewCustomResourceDefinitionsServerOptions creates default options of an apiextensions-apiserver.
func NewCustomResourceDefinitionsServerOptions(out, errOut io.Writer) *CustomResourceDefinitionsServerOptions {
	o := &CustomResourceDefinitionsServerOptions{
		ServerRunOptions: options.NewServerRunOptions(),
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			apiserver.Codecs.LegacyCodec(v1beta1.SchemeGroupVersion, v1.SchemeGroupVersion),
		),
		APIEnablement: genericoptions.NewAPIEnablementOptions(),

		StdOut: out,
		StdErr: errOut,
	}
	o.RecommendedOptions.ExtraAdmissionInitializers = func(c *genericapiserver.RecommendedConfig) ([]admission.PluginInitializer, error) {
		// TODO: wire in access to crdInfo for use with CEL admission here?
		// It's possible to wire in an informer for CRDs. Since it would be a shared informer, the overhead
		// is quite minimal if it's used judicially. It's still lame, but it could work, at least for a prototype.

		crdClient, err := apiextensionsclient.NewForConfig(c.LoopbackClientConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create clientset: %v", err)
		}
		informerFactory := apiextensionsinformers.NewSharedInformerFactory(crdClient, 5*time.Minute)

		return []admission.PluginInitializer{celAdmissionInitializer{runtime: newExpressionRuntime(informerFactory)}}, nil
	}

	return o
}

func newExpressionRuntime(informerFactory apiextensionsinformers.SharedInformerFactory) celhandlers.ExpressionRuntime {
	ret := crdExpressionRuntime{}
	informerFactory.Apiextensions().V1().CustomResourceDefinitions().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ret.createCustomResourceDefinition,
		UpdateFunc: ret.updateCustomResourceDefinition,
		DeleteFunc: ret.removeCustomResourceDefinition,
	})
	return ret
}

type crdExpressionRuntime struct {
	schemas  map[schema.GroupVersionKind]*structuralschema.Structural
	celTypes map[schema.GroupVersionKind]*exprpb.Type
}

func (c crdExpressionRuntime) createCustomResourceDefinition(obj interface{}) {
	crd := obj.(apiextensions.CustomResourceDefinition)
	for _, v := range crd.Spec.Versions {
		gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: v.Name, Kind: crd.Spec.Names.Kind}
		structural, err := structuralschema.NewStructural(v.Schema.OpenAPIV3Schema)
		if err != nil {
			// TODO
		}
		c.schemas[gvk] = structural
		declType := celmodel.SchemaDeclType(structural, true)
		c.celTypes[gvk] = declType.ExprType()
	}
}

func (c crdExpressionRuntime) updateCustomResourceDefinition(oldObj, newObj interface{}) {
	oldCrd := oldObj.(apiextensions.CustomResourceDefinition)
	crd := newObj.(apiextensions.CustomResourceDefinition)
	newVersions := map[schema.GroupVersionKind]struct{}{}
	for _, v := range crd.Spec.Versions {
		gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: v.Name, Kind: crd.Spec.Names.Kind}
		newVersions[gvk] = struct{}{}
		structural, err := structuralschema.NewStructural(v.Schema.OpenAPIV3Schema)
		if err != nil {
			// TODO
		}
		c.schemas[gvk] = structural
		declType := celmodel.SchemaDeclType(structural, true)
		c.celTypes[gvk] = declType.ExprType()
	}
	for _, v := range oldCrd.Spec.Versions {
		gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: v.Name, Kind: crd.Spec.Names.Kind}
		if _, ok := newVersions[gvk]; !ok {
			delete(c.schemas, gvk)
			delete(c.celTypes, gvk)
		}
	}
}

func (c crdExpressionRuntime) removeCustomResourceDefinition(obj interface{}) {
	crd := obj.(apiextensions.CustomResourceDefinition)
	for _, v := range crd.Spec.Versions {
		gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: v.Name, Kind: crd.Spec.Names.Kind}
		delete(c.schemas, gvk)
		delete(c.celTypes, gvk)
	}
}

func (c crdExpressionRuntime) Compile(rule string, gvk schema.GroupVersionKind) (cel.Program, error) {
	s := c.schemas[gvk]
	isRootResource := true
	var propDecls []*exprpb.Decl
	var root *celmodel.DeclType
	var ok bool
	env, err := cel.NewEnv(
		cel.HomogeneousAggregateLiterals(),
	)
	if err != nil {
		return nil, err
	}
	reg := celmodel.NewRegistry(env)
	scopedTypeName := "xxxxxx"
	rt, err := celmodel.NewRuleTypes(scopedTypeName, s, isRootResource, reg)
	if err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, nil
	}
	opts, err := rt.EnvOptions(env.TypeProvider())
	if err != nil {
		return nil, err
	}
	root, ok = rt.FindDeclType(scopedTypeName)
	if !ok {
		rootDecl := celmodel.SchemaDeclType(s, isRootResource)
		if rootDecl == nil {
			return nil, fmt.Errorf("rule declared on schema that does not support validation rules type: '%s' x-kubernetes-preserve-unknown-fields: '%t'", s.Type, s.XPreserveUnknownFields)
		}
		root = rootDecl.MaybeAssignTypeName(scopedTypeName)
	}
	propDecls = append(propDecls, decls.NewVar("self", root.ExprType()))
	//propDecls = append(propDecls, decls.NewVar(OldScopedVarName, root.ExprType()))
	opts = append(opts, cel.Declarations(propDecls...), cel.HomogeneousAggregateLiterals())
	opts = append(opts, library.ExtensionLibs...)
	env, err = env.Extend(opts...)
	if err != nil {
		return nil, err
	}
	//estimator := newCostEstimator(root)
	ast, issues := env.Compile(rule)
	if issues != nil {
		return nil, fmt.Errorf("compilation failed: " + issues.String())
	}
	if !proto.Equal(ast.ResultType(), decls.Bool) {
		return nil, fmt.Errorf("cel expression must evaluate to a bool")
	}

	// TODO: Ideally we could configure the per expression limit at validation time and set it to the remaining overall budget, but we would either need a way to pass in a limit at evaluation time or move program creation to validation time
	prog, err := env.Program(ast,
		cel.EvalOptions(cel.OptOptimize, cel.OptTrackCost),
		//cel.CostLimit(perCallLimit),
		//cel.CostTracking(estimator),
		cel.OptimizeRegex(library.ExtensionLibRegexOptimizations...),
		//cel.InterruptCheckFrequency(checkFrequency),
	)
	return prog, nil
}

func (c crdExpressionRuntime) Eval(program cel.Program, object runtime.Object) (ref.Val, error) {
	in, err := c.objectToCelVal(object)
	if err != nil {
		return nil, err
	}
	result, _, err := program.Eval(in)
	return result, err
}

func (c crdExpressionRuntime) objectToCelVal(object runtime.Object) (ref.Val, error) {
	switch t := object.(type) {
	case *unstructured.Unstructured:
		s := c.schemas[object.GetObjectKind().GroupVersionKind()]
		return schemacel.UnstructuredToVal(t.Object, s), nil
	}
	return nil, fmt.Errorf("unsupported type")
}

func (c crdExpressionRuntime) objectKindToCelType(kind schema.ObjectKind) *exprpb.Type {
	return c.celTypes[kind.GroupVersionKind()]
}

type celAdmissionInitializer struct {
	runtime celhandlers.ExpressionRuntime
}

func (c celAdmissionInitializer) Initialize(plugin admission.Interface) {
	if wants, ok := plugin.(initializer.WantsExpressionRuntime); ok {
		wants.SetExpressionRuntime(c.runtime)
	}
}

// AddFlags adds the apiextensions-apiserver flags to the flagset.
func (o CustomResourceDefinitionsServerOptions) AddFlags(fs *pflag.FlagSet) {
	o.ServerRunOptions.AddUniversalFlags(fs)
	o.RecommendedOptions.AddFlags(fs)
	o.APIEnablement.AddFlags(fs)
}

// Validate validates the apiextensions-apiserver options.
func (o CustomResourceDefinitionsServerOptions) Validate() error {
	errors := []error{}
	errors = append(errors, o.ServerRunOptions.Validate()...)
	errors = append(errors, o.RecommendedOptions.Validate()...)
	errors = append(errors, o.APIEnablement.Validate(apiserver.Scheme)...)
	return utilerrors.NewAggregate(errors)
}

// Complete fills in missing options.
func (o *CustomResourceDefinitionsServerOptions) Complete() error {
	return nil
}

// Config returns an apiextensions-apiserver configuration.
func (o CustomResourceDefinitionsServerOptions) Config() (*apiserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)
	if err := o.ServerRunOptions.ApplyTo(&serverConfig.Config); err != nil {
		return nil, err
	}
	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}
	if err := o.APIEnablement.ApplyTo(&serverConfig.Config, apiserver.DefaultAPIResourceConfigSource(), apiserver.Scheme); err != nil {
		return nil, err
	}

	config := &apiserver.Config{
		GenericConfig: serverConfig,
		ExtraConfig: apiserver.ExtraConfig{
			CRDRESTOptionsGetter: NewCRDRESTOptionsGetter(*o.RecommendedOptions.Etcd),
			ServiceResolver:      &serviceResolver{serverConfig.SharedInformerFactory.Core().V1().Services().Lister()},
			AuthResolverWrapper:  webhook.NewDefaultAuthenticationInfoResolverWrapper(nil, nil, serverConfig.LoopbackClientConfig, nil),
		},
	}
	return config, nil
}

// NewCRDRESTOptionsGetter create a RESTOptionsGetter for CustomResources.
func NewCRDRESTOptionsGetter(etcdOptions genericoptions.EtcdOptions) genericregistry.RESTOptionsGetter {
	ret := apiserver.CRDRESTOptionsGetter{
		StorageConfig:             etcdOptions.StorageConfig,
		StoragePrefix:             etcdOptions.StorageConfig.Prefix,
		EnableWatchCache:          etcdOptions.EnableWatchCache,
		DefaultWatchCacheSize:     etcdOptions.DefaultWatchCacheSize,
		EnableGarbageCollection:   etcdOptions.EnableGarbageCollection,
		DeleteCollectionWorkers:   etcdOptions.DeleteCollectionWorkers,
		CountMetricPollPeriod:     etcdOptions.StorageConfig.CountMetricPollPeriod,
		StorageObjectCountTracker: etcdOptions.StorageConfig.StorageObjectCountTracker,
	}
	ret.StorageConfig.Codec = unstructured.UnstructuredJSONScheme

	return ret
}

type serviceResolver struct {
	services corev1.ServiceLister
}

func (r *serviceResolver) ResolveEndpoint(namespace, name string, port int32) (*url.URL, error) {
	return proxy.ResolveCluster(r.services, namespace, name, port)
}
