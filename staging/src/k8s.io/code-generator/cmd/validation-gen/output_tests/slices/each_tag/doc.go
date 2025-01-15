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

// +k8s:validation-gen=TypeMeta
// +k8s:validation-gen-scheme-registry=k8s.io/code-generator/cmd/validation-gen/testscheme.Scheme
// +k8s:validation-gen-test-fixture=validateFalse

// This is a test package.
package each_tag

import "k8s.io/code-generator/cmd/validation-gen/testscheme"

var localSchemeBuilder = testscheme.New()

type T1 struct {
	TypeMeta int

	// +k8s:eachVal2=+k8s:validateFalse="T1.LS[*] #1"
	// +k8s:eachVal2=+k8s:validateFalse="T1.LS[*] #2"
	LS []string `json:"ls"`

	// +k8s:eachVal2=+k8s:validateFalse="T1.LT2[*] #1"
	// +k8s:eachVal2=+k8s:validateFalse="T1.LT2[*] #2"
	LT2 []T2 `json:"lt2"`

	// +k8s:eachVal2=+k8s:validateFalse="T1.LPT2[*] #1"
	// +k8s:eachVal2=+k8s:validateFalse="T1.LPT2[*] #2"
	LPT2 []*T2 `json:"lpt2"`

	// +k8s:listType2=map
	// +k8s:listMapKey2=Foo
	// +k8s:listMapKey2=Bar
	// +k8s:eachVal2=+k8s:validateFalse="T1.LMT2[*] #1"
	// +k8s:eachVal2=+k8s:validateFalse="T1.LMT2[*] #2"
	LMT2 []T2 `json:"lmt2"`

	// +k8s:listType2=map
	// +k8s:listMapKey2=Foo
	// +k8s:listMapKey2=Bar
	// +k8s:eachVal2=+k8s:validateFalse="T1.LMPT2[*] #1"
	// +k8s:eachVal2=+k8s:validateFalse="T1.LMPT2[*] #2"
	LMPT2 []*T2 `json:"lmpt2"`

	// +k8s:subfield(ls)=+k8s:eachVal2=+k8s:format=ip-sloppy
	// +k8s:subfield(ls)=+k8s:eachVal2=+k8s:format=dns-label
	Sub T3 `json:"sub"`

	// +k8s:eachVal2=+k8s:listType2=map
	// +k8s:eachVal2=+k8s:listMapKey2=Foo
	// +k8s:eachVal2=+k8s:listMapKey2=Bar
	// +k8s:eachVal2=+k8s:eachVal2=+k8s:validateFalse="T1.LLMT2[*] #1"
	// +k8s:eachVal2=+k8s:eachVal2=+k8s:validateFalse="T1.LLMT2[*] #2"
	LLMT2 [][]T2 `json:"llmt2"`
}

type T2 struct {
	Foo string `json:"foo"`
	Bar string `json:"bar"`
}

type T3 struct {
	LS []string `json:"ls"`
}

// FIXME: move to a better place
type T4 struct {
	TypeMeta int

	// +k8s:eachVal2=+k8s:validateFalse="T4.MSS[*]"
	MSS map[string]string `json:"mss"`

	// +k8s:eachVal2=+k8s:validateFalse="T4.MSPS[*]"
	MSPS map[string]*string `json:"msps"`
}
