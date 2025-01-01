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

package validators

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

const (
	// These tags return a fixed pass/fail state.
	validateTrueTagName  = "k8s:validateTrue"
	validateFalseTagName = "k8s:validateFalse"

	// This tag always returns an error from ExtractValidations.
	validateErrorTagName = "k8s:validateError"
)

func init() {
	RegisterTagDescriptor(fixedResultTag{result: true})
	RegisterTagDescriptor(fixedResultTag{result: false})
	RegisterTagDescriptor(fixedResultTag{error: true})
}

type fixedResultTag struct {
	result bool
	error  bool
}

var _ TagDescriptor = fixedResultTag{}

func (fixedResultTag) Init(_ *generator.Context) {}

func (frt fixedResultTag) TagName() string {
	if frt.error {
		return validateErrorTagName
	} else if frt.result {
		return validateTrueTagName
	}
	return validateFalseTagName
}

var fixedResultTagScopes = sets.New(TagScopeAll)

func (fixedResultTag) ValidScopes() sets.Set[TagScope] {
	return fixedResultTagScopes
}

func (frt fixedResultTag) GetValidations(context TagContext2, _ []string, payload string) (Validations, error) {
	var result Validations

	if frt.error {
		return result, fmt.Errorf("forced error: %q", payload)
	}

	tag, err := frt.parseTagPayload(payload)
	if err != nil {
		return result, fmt.Errorf("can't decode tag payload: %w", err)
	}
	result.AddFunction(GenericFunction(frt.TagName(), tag.flags, fixedResultValidator, tag.typeArgs, frt.result, tag.msg))

	return result, nil
}

var (
	fixedResultValidator = types.Name{Package: libValidationPkg, Name: "FixedResult"}
)

type fixedResultPayload struct {
	flags    FunctionFlags
	msg      string
	typeArgs []types.Name
}

func (fixedResultTag) parseTagPayload(in string) (fixedResultPayload, error) {
	type payload struct {
		Flags   []string `json:"flags"`
		Msg     string   `json:"msg"`
		TypeArg string   `json:"typeArg,omitempty"`
	}
	// We expect either a string (maybe empty) or a JSON object.
	if len(in) == 0 {
		return fixedResultPayload{}, nil
	}
	var pl payload
	if err := json.Unmarshal([]byte(in), &pl); err != nil {
		s := ""
		if err := json.Unmarshal([]byte(in), &s); err != nil {
			return fixedResultPayload{}, fmt.Errorf("error parsing JSON value: %v (%q)", err, in)
		}
		return fixedResultPayload{msg: s}, nil
	}
	// The msg field is required in JSON mode.
	if pl.Msg == "" {
		return fixedResultPayload{}, fmt.Errorf("JSON msg is required")
	}
	var flags FunctionFlags
	for _, fl := range pl.Flags {
		switch fl {
		case "ShortCircuit":
			flags |= ShortCircuit
		case "NonError":
			flags |= NonError
		default:
			return fixedResultPayload{}, fmt.Errorf("unknown flag: %q", fl)
		}
	}
	var typeArgs []types.Name
	if tn := pl.TypeArg; len(tn) > 0 {
		if !strings.HasPrefix(tn, "*") {
			tn = "*" + tn // We always need the pointer type.
		}
		typeArgs = []types.Name{{Package: "", Name: tn}}
	}

	return fixedResultPayload{flags, pl.Msg, typeArgs}, nil
}

func (frt fixedResultTag) Docs() []TagDoc {
	if frt.error {
		return []TagDoc{{
			Tag:         validateErrorTagName,
			Description: "Always fails code generation (useful for testing).",
			Contexts:    []TagScope{TagScopeType, TagScopeField},
			Payloads: []TagPayloadDoc{{
				Description: "<string>",
				Docs:        "This string will be included in the error message.",
			}},
		}}
	}

	if frt.result {
		return []TagDoc{{
			Tag:         validateTrueTagName,
			Description: "Always passes validation (useful for testing).",
			Contexts:    []TagScope{TagScopeType, TagScopeField},
			Payloads: []TagPayloadDoc{{
				Description: "<none>",
				Docs:        "",
			}, {
				Description: "<quoted-string>",
				Docs:        "The generated code will include this string.",
			}, {
				Description: "<json-object>",
				Docs:        "",
				Schema: []TagPayloadSchema{{
					Key:   "flags",
					Value: "<list-of-flag-string>",
					Docs:  `values: ShortCircuit, NonError`,
				}, {
					Key:   "msg",
					Value: "<string>",
					Docs:  "The generated code will include this string.",
				}, {
					Key:   "typeArg",
					Value: "<string>",
					Docs:  "The type arg in generated code (must be the value-type, not pointer).",
				}},
			}},
		}}
	} else {
		return []TagDoc{{
			Tag:         validateFalseTagName,
			Description: "Always fails validation (useful for testing).",
			Contexts:    []TagScope{TagScopeType, TagScopeField},
			Payloads: []TagPayloadDoc{{
				Description: "<none>",
				Docs:        "",
			}, {
				Description: "<quoted-string>",
				Docs:        "The generated code will include this string.",
			}, {
				Description: "<json-object>",
				Docs:        "",
				Schema: []TagPayloadSchema{{
					Key:   "flags",
					Value: "<list-of-flag-string>",
					Docs:  `values: ShortCircuit, NonError`,
				}, {
					Key:   "msg",
					Value: "<string>",
					Docs:  "The generated code will include this string.",
				}},
			}},
		}}
	}
}
