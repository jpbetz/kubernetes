/*
Copyright 2019 The Kubernetes Authors.

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

package main

import (
	"fmt"
	"sort"

	flag "github.com/spf13/pflag"

	openapi_v2 "github.com/googleapis/gnostic/OpenAPIv2"
	"k8s.io/klog"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	master     = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
)

func main() {
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags(*master, *kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
		return
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
		return
	}

	dc := discovery.NewDiscoveryClient(kubeClient.RESTClient())
	doc, err := dc.OpenAPISchema()
	if err != nil {
		klog.Fatalf("Error fetching OpenAPI spec: %s", err.Error())
		return
	}

	set := map[string]struct{}{}
	for _, prop := range doc.Definitions.AdditionalProperties {
		collectStrings(prop.Value, set)
	}
	strings := order(set)
	strings = trim(strings)
	for _, s := range strings {
		fmt.Printf("%s\n", s)
	}
}

// collectStrings traverses all the types in the schema and collects all strings
// into the set.
func collectStrings(schema *openapi_v2.Schema, set map[string]struct{}) {
	if schema.Type == nil {
		return
	}
	for _, t := range schema.Type.Value {
		switch t {
		case "object":
			if schema.Properties != nil {
				for _, namedSchema := range schema.Properties.AdditionalProperties {
					set[namedSchema.Name] = struct{}{}
					collectStrings(namedSchema.Value, set)
				}
			}
		case "array":
			for _, schema := range schema.Items.Schema {
				collectStrings(schema, set)
			}
		case "string":
		case "number":
		case "integer":
		case "boolean":
		default:
			klog.Fatalf("Unsupported type: %s", t)
		}
	}
}

// order sorts the strings primarily by length, and lexically if the strings are the same
// length.
func order(set map[string]struct{}) []string {
	result := make([]string, len(set), len(set))
	i := 0
	for s := range set {
		result[i] = s
		i++
	}
	sort.SliceStable(result, func(i, j int) bool {
		cmp := len(result[i]) - len(result[j])
		if cmp == 0 {
			return result[i] < result[j] // lexically sort equal length strings
		}
		return cmp < 0
	})
	return result
}

// trim removes any strings that are not longer than the `!<base64>` they would be replaced
// by.
func trim(strings []string) []string {
	results := make([]string, 0, len(strings))
	for i, s := range strings {
		encoded := toBase64(uint64(i))

		//fmt.Printf("!%s -> \"%s\"\n", encoded, s)
		if len(s)+2 >= len(encoded)+1 {
			results = append(results, s)
		}
	}
	return results
}

var encodeStd = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/")

// TODO: just need to calculate length. Computing the string is only helpful for debugging here.
// TODO: Use a standard library instead here. strconv.ParseInt only suupports up to base 36.
// base64.Encoder should work? https://play.golang.org/p/2gi8cjAAzmX
func toBase64(i uint64) string {
	var result []rune
	for i > 0 {
		result = append([]rune{encodeStd[i&63]}, result...)
		i = i >> 8
	}
	return string(result)
}
