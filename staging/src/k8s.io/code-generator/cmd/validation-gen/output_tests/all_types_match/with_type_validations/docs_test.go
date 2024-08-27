package withtypevalidations

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	pointer "k8s.io/utils/ptr"
)

// TODO: Replace this with a more convenient test utility. This is just a temporary sanity check.
func Test(t *testing.T) {
	expectValid := []any{
		&T2{},
		pointer.To(E2("x")),
	}

	expectInvalid := []struct {
		in   any
		want sets.Set[string]
	}{
		{
			in:   &T1{S: "x"},
			want: sets.New("forced failure: type T1", "forced failure: field T1.S"),
		},
		{
			in:   pointer.To(E1("x")),
			want: sets.New("forced failure: type E1"),
		},
	}

	for _, v := range expectValid {
		if len(localSchemeBuilder.Validate(v)) > 0 {
			t.Errorf("expected no validation errors")
		}
		if len(localSchemeBuilder.ValidateUpdate(v, v)) > 0 {
			t.Errorf("expected no validation errors")
		}
	}

	for _, v := range expectInvalid {
		t.Run(fmt.Sprintf("%#+v", v.in), func(t *testing.T) {
			got := localSchemeBuilder.Validate(v.in)
			gotMsgs := sets.New[string]()
			for _, g := range got {
				gotMsgs.Insert(g.Detail)
			}
			if !v.want.Equal(gotMsgs) {
				t.Errorf("want validation %v but got %v", v.want, gotMsgs)
			}
		})
	}
}
