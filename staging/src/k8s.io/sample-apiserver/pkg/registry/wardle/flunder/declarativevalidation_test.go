package flunder

import (
	"testing"

	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	wardle "k8s.io/sample-apiserver/pkg/apis/wardle/v1alpha1"
)

func TestDeclarativeValidation(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.OpenAPIEnums, true)()

	strategy := NewStrategy(nil)
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(),
		&genericapirequest.RequestInfo{
			APIGroup:   wardle.SchemeGroupVersion.Group,
			APIVersion: wardle.SchemeGroupVersion.Version,
		},
	)

	configuration := validResource()
	strategy.PrepareForCreate(ctx, configuration)
	errs := strategy.Validate(ctx, configuration)
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf(err.Error())
		}
	}
}

func validResource() *wardle.Flunder {
	return &wardle.Flunder{
		Spec: wardle.FlunderSpec{
			Reference: "ref",
		},
	}
}
