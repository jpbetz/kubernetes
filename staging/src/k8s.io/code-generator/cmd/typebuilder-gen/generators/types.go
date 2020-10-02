package generators

import "k8s.io/gengo/types"

var (
	unstructured    = types.Name{Package: "k8s.io/apimachinery/pkg/apis/meta/v1", Name: "Unstructured"}
	unstructuredList    = types.Name{Package: "k8s.io/apimachinery/pkg/apis/meta/v1", Name: "UnstructuredList"}
)