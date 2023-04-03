/*
Copyright 2023 The Kubernetes Authors.

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

package library

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/kube-openapi/pkg/validation/strfmt"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Identifiers() cel.EnvOption {
	return cel.Lib(identifiersLib)
}

var identifiersLib = &identifiers{}

type identifiers struct{}

var identifiersLibraryDecls = map[string][]cel.FunctionOpt{
	"isFormat": {
		cel.MemberOverload("is_format", []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
			cel.BinaryBinding(isFormat))},
	"isGenerateNameOfFormat": {
		cel.MemberOverload("is_generate_name_of_format", []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
			cel.BinaryBinding(isGenerateNameOfFormat))},

	// TODO
	// validateFormat() - returns list of strings of any format violations
	// validateGenerateNameOfFormat()

	// TODO
	// <string>.isIP() <bool>
	// ip(<string>) <IP (dual stack)>
	// <IP>.isv4() <bool>
	// <IP>.isv6() <bool>
	// ipv4(<string>) <IPv4>
	// ipv6(<string>) <IPv6>
	// TODO: what utility functions are needed?  loopback() ?
}

func (*identifiers) CompileOptions() []cel.EnvOption {
	options := make([]cel.EnvOption, 0, len(identifiersLibraryDecls))
	for name, overloads := range identifiersLibraryDecls {
		options = append(options, cel.Function(name, overloads...))
	}
	return options
}

func (*identifiers) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// TODO: move to a reasonable package for export
var Registry = strfmt.NewFormats()

func init() {
	dns1123Subdomain := stringformat("")
	Registry.Add("dns1123subdomain", &dns1123Subdomain, isDNS1123Subdomain)

	dns1035Label := stringformat("")
	Registry.Add("dns1035label", &dns1035Label, isDNS1035Label)

	dns1123Label := stringformat("")
	Registry.Add("dns1123label", &dns1123Label, isDNS1123Label)
}

func isFormat(arg1, arg2 ref.Val) ref.Val {
	identifier, ok := arg1.Value().(string)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg1)
	}
	format, ok := arg2.Value().(string)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg2)
	}

	if ok := Registry.ContainsName(format); !ok {
		return types.NewErr(fmt.Sprintf("invalid format: %s", format))
	}
	valid := Registry.Validates(format, identifier)
	return types.Bool(valid)
}

func isGenerateNameOfFormat(arg1, arg2 ref.Val) ref.Val {
	identifier, ok := arg1.Value().(string)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg1)
	}
	format, ok := arg2.Value().(string)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg2)
	}

	var toGenerateName func(string) string
	switch format {
	case "dns1123subdomain", "dns1123label", "dns1035label":
		toGenerateName = maskTrailingDash
	default:
		return types.NewErr(fmt.Sprintf("unsupported format for generate name: %s", format))
	}

	if ok := Registry.ContainsName(format); !ok {
		return types.NewErr(fmt.Sprintf("invalid format: %s", format))
	}

	valid := Registry.Validates(format, toGenerateName(identifier))
	return types.Bool(valid)
}

// maskTrailingDash replaces the final character of a string with a subdomain safe
// value if it is a dash and if the length of this string is greater than 1. Note that
// this is used when a value could be appended to the string, see ValidateNameFunc
// for more info.
func maskTrailingDash(name string) string {
	if len(name) > 1 && strings.HasSuffix(name, "-") {
		return name[:len(name)-2] + "a"
	}
	return name
}

func isDNS1123Subdomain(value string) bool {
	return len(validation.IsDNS1123Subdomain(value)) == 0
}

func isDNS1035Label(value string) bool {
	return len(validation.IsDNS1035Label(value)) == 0
}

func isDNS1123Label(value string) bool {
	return len(validation.IsDNS1123Label(value)) == 0
}

func isWildcardDNS1123Subdomain(value string) bool {
	return len(validation.IsWildcardDNS1123Subdomain(value)) == 0
}

func isQualifiedName(value string) bool {
	return len(validation.IsQualifiedName(value)) == 0
}

func isFullyQualifiedName(value string) bool {
	return len(validation.IsFullyQualifiedName(field.NewPath("ignored"), value)) == 0
}

func isDomainPrefixedPath(value string) bool {
	return len(validation.IsFullyQualifiedName(field.NewPath("ignored"), value)) == 0
}

func isValidLabelValue(value string) bool {
	return len(validation.IsValidLabelValue(value)) == 0
}

type stringformat string

func (u stringformat) MarshalText() ([]byte, error) {
	return []byte(string(u)), nil
}

func (u *stringformat) UnmarshalText(data []byte) error { // validation is performed later on
	*u = stringformat(string(data))
	return nil
}

func (u *stringformat) Scan(raw interface{}) error {
	switch v := raw.(type) {
	case []byte:
		*u = stringformat(string(v))
	case string:
		*u = stringformat(v)
	default:
		return fmt.Errorf("cannot sql.Scan() strfmt.SSN from: %#v", v)
	}

	return nil
}

func (u stringformat) String() string {
	return string(u)
}

// MarshalJSON returns the string as JSON
func (u stringformat) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(u))
}

// UnmarshalJSON sets the string from JSON
func (u *stringformat) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var ustr string
	if err := json.Unmarshal(data, &ustr); err != nil {
		return err
	}
	*u = stringformat(ustr)
	return nil
}

// DeepCopyInto copies the receiver and writes its value into out.
func (u *stringformat) DeepCopyInto(out *stringformat) {
	*out = *u
}

// DeepCopy copies the receiver into a new string.
func (u *stringformat) DeepCopy() *stringformat {
	if u == nil {
		return nil
	}
	out := new(stringformat)
	u.DeepCopyInto(out)
	return out
}
