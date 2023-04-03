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

package model

import (
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiserver/pkg/cel/common"
)

var _ common.Schema = (*Structural)(nil)
var _ common.SchemaOrBool = (*StructuralOrBool)(nil)

type Structural struct {
	Structural *schema.Structural
}

type StructuralOrBool struct {
	StructuralOrBool *schema.StructuralOrBool
}

func (sb *StructuralOrBool) Schema() common.Schema {
	if sb.StructuralOrBool.Structural == nil {
		return nil
	}
	return &Structural{Structural: sb.StructuralOrBool.Structural}
}

func (sb *StructuralOrBool) Allows() bool {
	return sb.StructuralOrBool.Bool
}

func (s *Structural) Type() string {
	return s.Structural.Type
}

func (s *Structural) Format() string {
	if s.Structural.ValueValidation == nil {
		return ""
	}
	return s.Structural.ValueValidation.Format
}

func (s *Structural) Pattern() string {
	if s.Structural.ValueValidation == nil {
		return ""
	}
	return s.Structural.ValueValidation.Pattern
}

func (s *Structural) Items() common.Schema {
	return &Structural{Structural: s.Structural.Items}
}

func (s *Structural) Properties() map[string]common.Schema {
	if s.Structural.Properties == nil {
		return nil
	}
	res := make(map[string]common.Schema, len(s.Structural.Properties))
	for n, prop := range s.Structural.Properties {
		s := prop
		res[n] = &Structural{Structural: &s}
	}
	return res
}

func (s *Structural) AdditionalProperties() common.SchemaOrBool {
	if s.Structural.AdditionalProperties == nil {
		return nil
	}
	return &StructuralOrBool{StructuralOrBool: s.Structural.AdditionalProperties}
}

func (s *Structural) Default() any {
	return s.Structural.Default.Object
}

func (s *Structural) Minimum() *float64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.Minimum
}

func (s *Structural) ExclusiveMinimum() bool {
	if s.Structural.ValueValidation == nil {
		return false
	}
	return s.Structural.ValueValidation.ExclusiveMinimum
}

func (s *Structural) Maximum() *float64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.Maximum
}

func (s *Structural) ExclusiveMaximum() bool {
	if s.Structural.ValueValidation == nil {
		return false
	}
	return s.Structural.ValueValidation.ExclusiveMaximum
}

func (s *Structural) MultipleOf() *float64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MultipleOf
}

func (s *Structural) MinItems() *int64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MinItems
}

func (s *Structural) MaxItems() *int64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MaxItems
}

func (s *Structural) MinLength() *int64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MinLength
}

func (s *Structural) MaxLength() *int64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MaxLength
}

func (s *Structural) MinProperties() *int64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MinProperties
}

func (s *Structural) MaxProperties() *int64 {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.MaxProperties
}

func (s *Structural) Required() []string {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	return s.Structural.ValueValidation.Required
}

func (s *Structural) Enum() []any {
	if s.Structural.ValueValidation == nil {
		return nil
	}
	ret := make([]any, 0, len(s.Structural.ValueValidation.Enum))
	for _, e := range s.Structural.ValueValidation.Enum {
		ret = append(ret, e.Object)
	}
	return ret
}

func (s *Structural) Nullable() bool {
	return s.Structural.Nullable
}

func (s *Structural) IsXIntOrString() bool {
	return s.Structural.XIntOrString
}

func (s *Structural) IsXEmbeddedResource() bool {
	return s.Structural.XEmbeddedResource
}

func (s *Structural) IsXPreserveUnknownFields() bool {
	return s.Structural.XPreserveUnknownFields
}

func (s *Structural) XListType() string {
	if s.Structural.XListType == nil {
		return ""
	}
	return *s.Structural.XListType
}

func (s *Structural) XMapType() string {
	if s.Structural.XMapType == nil {
		return ""
	}
	return *s.Structural.XMapType
}

func (s *Structural) XListMapKeys() []string {
	return s.Structural.XListMapKeys
}

type StructuralValidationRule struct {
	rule, message, messageExpression string
}

func (s *StructuralValidationRule) Rule() string {
	return s.rule
}
func (s *StructuralValidationRule) Message() string {
	return s.message
}
func (s *StructuralValidationRule) MessageExpression() string {
	return s.messageExpression
}

func (s *Structural) XValidations() []common.ValidationRule {
	if len(s.Structural.XValidations) == 0 {
		return nil
	}
	result := make([]common.ValidationRule, len(s.Structural.XValidations))
	for i, v := range s.Structural.XValidations {
		result[i] = &StructuralValidationRule{rule: v.Rule, message: v.Message, messageExpression: v.MessageExpression}
	}
	return result
}

func (s *Structural) WithTypeAndObjectMeta() common.Schema {
	return &Structural{Structural: WithTypeAndObjectMeta(s.Structural)}
}
