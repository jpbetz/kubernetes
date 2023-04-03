package validation

import (
	"encoding/json"
	"os"
	"testing"

	"sigs.k8s.io/yaml"

	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/apivalidation"
	openapiresolver "k8s.io/apiserver/pkg/cel/openapi/resolver"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/sample-apiserver/pkg/apis/wardle/v1alpha1"
	clientScheme "k8s.io/sample-apiserver/pkg/generated/clientset/versioned/scheme"
	"k8s.io/sample-apiserver/pkg/generated/openapi"

	_ "k8s.io/sample-apiserver/pkg/apis/wardle/install"
)

func TestDeclarativeValidation(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.OpenAPIEnums, true)()

	schemaResolver := openapiresolver.NewDefinitionsSchemaResolver(clientScheme.Scheme, openapi.GetOpenAPIDefinitions)
	declarativeValidator := apivalidation.NewDeclarativeValidator(schemaResolver, celconfig.PerCallLimit)

	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(),
		&genericapirequest.RequestInfo{
			APIGroup:   v1alpha1.SchemeGroupVersion.Group,
			APIVersion: v1alpha1.SchemeGroupVersion.Version,
		},
	)

	yamlFile, err := os.ReadFile("testdata/01-flunder.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var flunder v1alpha1.Flunder
	j, err := yaml.YAMLToJSON(yamlFile)
	if err != nil {
		t.Fatal(err)
	}
	err = json.Unmarshal(j, &flunder)
	if err != nil {
		t.Fatal(err)
	}
	errs, _ := declarativeValidator.Validate(ctx, &flunder, nil, celconfig.PerCallLimit)
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf(err.Error())
		}
	}
}
