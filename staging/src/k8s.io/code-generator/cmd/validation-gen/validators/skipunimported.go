/*
Copyright 2025 The Kubernetes Authors.

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

package validators

import "k8s.io/apimachinery/pkg/util/sets"

const (
	skipUnimportedTagName = "k8s:skipUnimported"
)

type skipUnimported struct{}

func init() {
	RegisterTagValidator(skipUnimported{})
}

func (skipUnimported) Init(Config) {}

func (skipUnimported) TagName() string {
	return skipUnimportedTagName
}

func (skipUnimported) ValidScopes() sets.Set[Scope] {
	return sets.New(ScopeAny)
}

func (skipUnimported) GetValidations(context Context, _ []string, _ string) (Validations, error) {
	return Validations{SkipUnimported: true}, nil
}

func (skipUnimported) Docs() TagDoc {
	doc := TagDoc{
		Tag:    skipUnimportedTagName,
		Scopes: []Scope{ScopeField},
		Description: `Indicates that validations declared on the field's type will be skipped when the type is not imported. 
If a type is not imported and this tag is not set, the generator will fail generation to prevent the generator
from silently skipping validation. If the validation should not be skipped, add the type's package to the generator
using the --extra-pkg flag.`,
	}
	return doc

}
