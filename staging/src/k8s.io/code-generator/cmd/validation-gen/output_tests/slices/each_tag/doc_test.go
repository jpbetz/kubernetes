/*
Copyright 2024 The Kubernetes Authors.

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
package each_tag

import (
	"testing"
)

// FIXME: find the right home for this
func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	t2elem0 := T2{Foo: "foo0", Bar: "bar0"}
	t2elem1 := T2{Foo: "foo1", Bar: "bar1"}

	st.Value(&T1{
		LS:    []string{"zero", "one"},
		LT2:   []T2{t2elem0, t2elem1},
		LPT2:  []*T2{&t2elem0, &t2elem1},
		LMT2:  []T2{t2elem0, t2elem1},
		LMPT2: []*T2{&t2elem0, &t2elem1},
		Sub:   T3{LS: []string{"no good zero", "no good one"}},
		LLMT2: [][]T2{{t2elem0, t2elem1}, {t2elem0, t2elem1}},
	}).
		ExpectValidateFalseByPath(map[string][]string{
			"ls[0]": []string{"T1.LS[*] #1", "T1.LS[*] #2"},
			"ls[1]": []string{"T1.LS[*] #1", "T1.LS[*] #2"},

			"lt2[0]": []string{"T1.LT2[*] #1", "T1.LT2[*] #2"},
			"lt2[1]": []string{"T1.LT2[*] #1", "T1.LT2[*] #2"},

			"lpt2[0]": []string{"T1.LPT2[*] #1", "T1.LPT2[*] #2"},
			"lpt2[1]": []string{"T1.LPT2[*] #1", "T1.LPT2[*] #2"},

			"lmt2[0]": []string{"T1.LMT2[*] #1", "T1.LMT2[*] #2"},
			"lmt2[1]": []string{"T1.LMT2[*] #1", "T1.LMT2[*] #2"},

			"lmpt2[0]": []string{"T1.LMPT2[*] #1", "T1.LMPT2[*] #2"},
			"lmpt2[1]": []string{"T1.LMPT2[*] #1", "T1.LMPT2[*] #2"},

			/*
				"sub[0]": []string{
					"must be a valid IP address (e.g. 10.9.8.7 or 2001:db8::ffff)",
					"must contain only lower-case alphanumeric characters or '-'",
				},
				"sub[1]": []string{
					"must be a valid IP address (e.g. 10.9.8.7 or 2001:db8::ffff)",
					"must contain only lower-case alphanumeric characters or '-'",
				},
			*/

			"llmt2[0][0]": []string{"T1.LLMT2[*] #1", "T1.LLMT2[*] #2"},
			"llmt2[0][1]": []string{"T1.LLMT2[*] #1", "T1.LLMT2[*] #2"},
			"llmt2[1][0]": []string{"T1.LLMT2[*] #1", "T1.LLMT2[*] #2"},
			"llmt2[1][1]": []string{"T1.LLMT2[*] #1", "T1.LLMT2[*] #2"},
		})

	st.Value(&T4{
		MSS: map[string]string{"zero": "000", "one": "111"},
	}).
		ExpectValidateFalseByPath(map[string][]string{
			"mss[zero]": []string{"T4.MSS[*]"},
			"mss[one]":  []string{"T4.MSS[*]"},
		})
}
