package generators

import "k8s.io/gengo/types"

var (
	unstructuredConverter    = types.Ref("k8s.io/apimachinery/pkg/runtime", "DefaultUnstructuredConverter")
)