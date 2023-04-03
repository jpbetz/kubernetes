/*
Copyright 2021 The Kubernetes Authors.

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

package apivalidation

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/metrics"
	"k8s.io/apiserver/pkg/cel/openapi"
	openapiresolver "k8s.io/apiserver/pkg/cel/openapi/resolver"
)

// NewDeclarativeValidator creates a DeclarativeValidator configured with the provide schema resolver.
// The returned DeclarativeValidator can validate any type that the resolve is able to resolve.
func NewDeclarativeValidator(resolver openapiresolver.SchemaResolver, perCallLimit uint64) *DeclarativeValidator {
	return &DeclarativeValidator{resolver: resolver, perCallLimit: perCallLimit, validators: atomic.Pointer[validatorsByGVK]{}}
}

type validatorsByGVK map[schema.GroupVersionKind]*Validator

type DeclarativeValidator struct {
	resolver     openapiresolver.SchemaResolver
	perCallLimit uint64
	validators   atomic.Pointer[validatorsByGVK]
}

func (d *DeclarativeValidator) validatorForGVK(gvk schema.GroupVersionKind) (*Validator, error) {
	ptr := d.validators.Load()
	if ptr != nil {
		validators := *ptr
		if validator, ok := validators[gvk]; ok {
			return validator, nil
		}
	}
	s, err := d.resolver.ResolveSchema(gvk)
	if err != nil {
		return nil, err
	}
	validator := NewValidator(&openapi.Schema{Schema: s}, true, d.perCallLimit)

	var existing validatorsByGVK
	if ptr != nil {
		existing = *ptr
	}
	replacement := make(validatorsByGVK, len(existing)+1)
	for k, v := range existing {
		replacement[k] = v
	}
	replacement[gvk] = validator
	d.validators.Store(&replacement)
	return validator, nil
}

// Validate validates all x-kubernetes-validations rules in Validator against the objects and returns any errors.
// If the validation rules exceed the costBudget, subsequent evaluations will be skipped, the list of errs returned will not be empty, and a negative remainingBudget will be returned.
// Most callers can ignore the returned remainingBudget value unless another validate call is going to be made
// context is passed for supporting context cancellation during cel validation
func (d *DeclarativeValidator) Validate(ctx context.Context, obj, oldObj runtime.Object, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	validator, err := d.validatorForGVK(obj.GetObjectKind().GroupVersionKind())
	if err != nil {
		errs = append(errs, field.InternalError(nil, fmt.Errorf("unexpected error loading declarative validator: %w", err)))
		return errs, -1
	}

	var unstructured, oldUnstructured any
	unstructured, err = runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		errs = append(errs, field.InternalError(nil, fmt.Errorf("unexpected error converting object to unstructured: %w", err)))
	}
	if oldObj != nil {
		oldUnstructured, err = runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			errs = append(errs, field.InternalError(nil, fmt.Errorf("unexpected error converting old object to unstructured: %w", err)))
		}
	}

	// TODO: How to skip status when validating only spec?
	// TODO: How to handle root fieldPath better? Right now it does not print errors nicely for root validations. (see what we did for CRDs).
	// TODO: How to ensure that metadata.name validations still report the field path to the metadata.name?  If we use a validator from the
	//       root we don't get this.

	return validator.Validate(ctx, nil, unstructured, oldUnstructured, costBudget)
}

// Validator parallels the structure of common.Schema and includes the compiled CEL programs
// for the x-kubernetes-validations of each schema node.
type Validator struct {
	schema common.Schema

	Items      *Validator
	Properties map[string]Validator

	AdditionalProperties *Validator

	compiledRules []CompilationResult

	// Program compilation is pre-checked at CRD creation/update time, so we don't expect compilation to fail
	// they are recompiled and added to this type, and it does, it is an internal bug.
	// But if somehow we get any compilation errors, we track them and then surface them as validation errors.
	compilationErr error

	// isResourceRoot is true if this validator node is for the root of a resource. Either the root of the
	// custom resource being validated, or the root of an XEmbeddedResource object.
	isResourceRoot bool

	// celActivationFactory produces an Activation, which resolves identifiers (e.g. self and
	// oldSelf) to CEL values.
	celActivationFactory func(sts common.Schema, obj, oldObj interface{}) interpreter.Activation
}

// NewValidator returns compiles all the CEL programs defined in x-kubernetes-validations extensions
// of the Schema and returns a custom resource validator that contains nested
// validators for all items, properties and additionalProperties that transitively contain validator rules.
// Returns nil if there are no validator rules in the Schema. May return a validator containing only errors.
// Adding perCallLimit as input arg for testing purpose only. Callers should always use const PerCallLimit from k8s.io/apiserver/pkg/apis/cel/config.go as input
func NewValidator(s common.Schema, isResourceRoot bool, perCallLimit uint64) *Validator {
	return validator(s, isResourceRoot, common.SchemaDeclType(s, isResourceRoot), perCallLimit)
}

// validator creates a Validator for all x-kubernetes-validations at the level of the provided schema and lower and
// returns the Validator if any x-kubernetes-validations exist in the schema, or nil if no x-kubernetes-validations
// exist. declType is expected to be a CEL DeclType corresponding to the Schema.
// perCallLimit was added for testing purpose only. Callers should always use const PerCallLimit from k8s.io/apiserver/pkg/apis/cel/config.go as input.
func validator(s common.Schema, isResourceRoot bool, declType *cel.DeclType, perCallLimit uint64) *Validator {
	// TODO: re enable this check once it is combined with a value validations check
	// TODO: make it possible to skip value validations while still performing a single pass for validation
	// if !hasXValidations(s) {
	// 	return nil
	// }

	compiledRules, err := Compile(s, declType, perCallLimit)
	var itemsValidator, additionalPropertiesValidator *Validator
	var propertiesValidators map[string]Validator
	if s.Items() != nil {
		itemsValidator = validator(s.Items(), s.Items().IsXEmbeddedResource(), declType.ElemType, perCallLimit)
	}
	if len(s.Properties()) > 0 {
		propertiesValidators = make(map[string]Validator, len(s.Properties()))
		for k, p := range s.Properties() {
			prop := p
			var fieldType *cel.DeclType
			if escapedPropName, ok := cel.Escape(k); ok {
				if f, ok := declType.Fields[escapedPropName]; ok {
					fieldType = f.Type
				} else {
					// fields with unknown types are omitted from CEL validation entirely
					continue
				}
			} else {
				// field may be absent from declType if the property name is unescapable, in which case we should convert
				// the field value type to a DeclType.
				fieldType = common.SchemaDeclType(prop, prop.IsXEmbeddedResource())
				if fieldType == nil {
					continue
				}
			}
			if p := validator(prop, prop.IsXEmbeddedResource(), fieldType, perCallLimit); p != nil {
				propertiesValidators[k] = *p
			}
		}
	}
	aProps := s.AdditionalProperties()
	if aProps != nil && aProps.Schema() != nil {
		aPropsSchema := aProps.Schema()
		additionalPropertiesValidator = validator(aPropsSchema, aPropsSchema.IsXEmbeddedResource(), declType.ElemType, perCallLimit)
	}
	if len(compiledRules) > 0 || hasValueValidations(s) || err != nil || itemsValidator != nil || additionalPropertiesValidator != nil || len(propertiesValidators) > 0 {
		var activationFactory = validationActivationWithoutOldSelf
		for _, rule := range compiledRules {
			if rule.TransitionRule {
				activationFactory = validationActivationWithOldSelf
				break
			}
		}

		return &Validator{
			schema:               s,
			compiledRules:        compiledRules,
			compilationErr:       err,
			isResourceRoot:       isResourceRoot,
			Items:                itemsValidator,
			AdditionalProperties: additionalPropertiesValidator,
			Properties:           propertiesValidators,
			celActivationFactory: activationFactory,
		}
	}

	return nil
}

func hasValueValidations(s common.Schema) bool {
	return s.Minimum() != nil || s.Maximum() != nil || s.MultipleOf() != nil ||
		len(s.Enum()) > 0 || len(s.Format()) > 0 || len(s.Pattern()) > 0 ||
		s.MinItems() != nil || s.MaxItems() != nil ||
		s.MinLength() != nil || s.MaxLength() != nil ||
		s.MinProperties() != nil || s.MaxProperties() != nil ||
		len(s.Required()) > 0
}

func (s *Validator) validateField(ctx context.Context, fieldName string, obj, oldObj runtime.Object, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	path := field.NewPath(fieldName)
	var unstructuredField, oldUnstructuredField any
	unstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		errs = append(errs, field.InternalError(path, fmt.Errorf("unexpected error converting object to unstructured: %w", err)))
	}
	unstructuredField, ok := unstructured[fieldName]
	if !ok {
		errs = append(errs, field.InternalError(path, fmt.Errorf("unexpected error accessing object field %s: %w", fieldName, err)))
	}
	if oldObj != nil {
		oldUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			errs = append(errs, field.InternalError(path, fmt.Errorf("unexpected error converting old object to unstructured: %w", err)))
		}
		oldUnstructuredField, ok = oldUnstructured[fieldName]
		if !ok {
			errs = append(errs, field.InternalError(path, fmt.Errorf("unexpected error accessing old object field %s: %w", fieldName, err)))
		}
	}
	fieldValidator, ok := s.Properties[fieldName]
	if !ok {
		errs = append(errs, field.InternalError(path, fmt.Errorf("unexpected error accessing field %s: %w", fieldName, err)))
	}

	return fieldValidator.Validate(ctx, path, unstructuredField, oldUnstructuredField, costBudget)
}

func (s *Validator) Validate(ctx context.Context, fldPath *field.Path, obj, oldObj interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {

	remainingBudget = costBudget
	if s == nil || obj == nil {
		return nil, remainingBudget
	}

	errs = validateValueValidations(fldPath, s.schema, obj)

	t := time.Now()
	defer func() {
		metrics.Metrics.ObserveEvaluation(time.Since(t))
	}()

	exprErrs, remainingBudget := s.validateExpressions(ctx, fldPath, s.schema, obj, oldObj, remainingBudget)
	errs = append(errs, exprErrs...)
	if remainingBudget < 0 {
		return errs, remainingBudget
	}
	switch obj := obj.(type) {
	case []interface{}:
		oldArray, _ := oldObj.([]interface{})
		var arrayErrs field.ErrorList
		arrayErrs, remainingBudget = s.validateArray(ctx, fldPath, s.schema, obj, oldArray, remainingBudget)
		errs = append(errs, arrayErrs...)
		return errs, remainingBudget
	case map[string]interface{}:
		oldMap, _ := oldObj.(map[string]interface{})
		var mapErrs field.ErrorList
		mapErrs, remainingBudget = s.validateMap(ctx, fldPath, obj, oldMap, remainingBudget)
		errs = append(errs, mapErrs...)
		return errs, remainingBudget
	}
	return errs, remainingBudget
}

func (s *Validator) validateExpressions(ctx context.Context, fldPath *field.Path, sts common.Schema, obj, oldObj interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	// guard against oldObj being a non-nil interface with a nil value
	if oldObj != nil {
		v := reflect.ValueOf(oldObj)
		switch v.Kind() {
		case reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
			if v.IsNil() {
				oldObj = nil // +k8s:verify-mutation:reason=clone
			}
		}
	}

	remainingBudget = costBudget
	if obj == nil {
		// We only validate non-null values. Rules that need to check for the state of a nullable value or the presence of an optional
		// field must do so from the surrounding schema. E.g. if an array has nullable string items, a rule on the array
		// schema can check if items are null, but a rule on the nullable string schema only validates the non-null strings.
		return nil, remainingBudget
	}
	if s.compilationErr != nil {
		errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("rule compiler initialization error: %v", s.compilationErr)))
		return errs, remainingBudget
	}
	if len(s.compiledRules) == 0 {
		return nil, remainingBudget // nothing to do
	}
	if remainingBudget <= 0 {
		errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("validation failed due to running out of cost budget, no further validation rules will be run")))
		return errs, -1
	}
	if s.isResourceRoot {
		sts = sts.WithTypeAndObjectMeta()
	}
	activation := s.celActivationFactory(sts, obj, oldObj)
	for i, compiled := range s.compiledRules {
		rule := sts.XValidations()[i]
		if compiled.Error != nil {
			errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("rule compile error: %v", compiled.Error)))
			continue
		}
		if compiled.Program == nil {
			// rule is empty
			continue
		}
		if compiled.TransitionRule && oldObj == nil {
			// transition rules are evaluated only if there is a comparable existing value
			continue
		}
		evalResult, evalDetails, err := compiled.Program.ContextEval(ctx, activation)
		if evalDetails == nil {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("runtime cost could not be calculated for validation rule: %v, no further validation rules will be run", ruleErrorString(rule))))
			return errs, -1
		} else {
			rtCost := evalDetails.ActualCost()
			if rtCost == nil {
				errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("runtime cost could not be calculated for validation rule: %v, no further validation rules will be run", ruleErrorString(rule))))
				return errs, -1
			} else {
				if *rtCost > math.MaxInt64 || int64(*rtCost) > remainingBudget {
					errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("validation failed due to running out of cost budget, no further validation rules will be run")))
					return errs, -1
				}
				remainingBudget -= int64(*rtCost)
			}
		}
		if err != nil {
			// see types.Err for list of well defined error types
			if strings.HasPrefix(err.Error(), "no such overload") {
				// Most overload errors are caught by the compiler, which provides details on where exactly in the rule
				// error was found. Here, an overload error has occurred at runtime no details are provided, so we
				// append a more descriptive error message. This error can only occur when static type checking has
				// been bypassed. int-or-string is typed as dynamic and so bypasses compiler type checking.
				errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("'%v': call arguments did not match a supported operator, function or macro signature for rule: %v", err, ruleErrorString(rule))))
			} else if strings.HasPrefix(err.Error(), "operation cancelled: actual cost limit exceeded") {
				errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("'%v': no further validation rules will be run due to call cost exceeds limit for rule: %v", err, ruleErrorString(rule))))
				return errs, -1
			} else {
				// no such key: {key}, index out of bounds: {index}, integer overflow, division by zero, ...
				errs = append(errs, field.Invalid(fldPath, sts.Type(), fmt.Sprintf("%v evaluating rule: %v", err, ruleErrorString(rule))))
			}
			continue
		}
		if evalResult != types.True {
			if compiled.MessageExpression != nil {
				messageExpression, newRemainingBudget, msgErr := evalMessageExpression(ctx, compiled.MessageExpression, rule.MessageExpression(), activation, remainingBudget)
				if msgErr != nil {
					if msgErr.Type == cel.ErrorTypeInternal {
						errs = append(errs, field.InternalError(fldPath, msgErr))
						return errs, -1
					} else if msgErr.Type == cel.ErrorTypeInvalid {
						errs = append(errs, field.Invalid(fldPath, sts.Type(), msgErr.Error()))
						return errs, -1
					} else {
						klog.V(2).ErrorS(msgErr, "messageExpression evaluation failed")
						errs = append(errs, field.Invalid(fldPath, sts.Type(), ruleMessageOrDefault(rule)))
						remainingBudget = newRemainingBudget
					}
				} else {
					errs = append(errs, field.Invalid(fldPath, sts.Type(), messageExpression))
					remainingBudget = newRemainingBudget
				}
			} else {
				errs = append(errs, field.Invalid(fldPath, sts.Type(), ruleMessageOrDefault(rule)))
			}
		}
	}
	return errs, remainingBudget
}

// evalMessageExpression evaluates the given message expression and returns the evaluated string form and the remaining budget, or an error if one
// occurred during evaluation.
func evalMessageExpression(ctx context.Context, expr celgo.Program, exprSrc string, activation interpreter.Activation, remainingBudget int64) (string, int64, *cel.Error) {
	evalResult, evalDetails, err := expr.ContextEval(ctx, activation)
	if evalDetails == nil {
		return "", -1, &cel.Error{
			Type:   cel.ErrorTypeInternal,
			Detail: fmt.Sprintf("runtime cost could not be calculated for messageExpression: %q", exprSrc),
		}
	}
	rtCost := evalDetails.ActualCost()
	if rtCost == nil {
		return "", -1, &cel.Error{
			Type:   cel.ErrorTypeInternal,
			Detail: fmt.Sprintf("runtime cost could not be calculated for messageExpression: %q", exprSrc),
		}
	} else if *rtCost > math.MaxInt64 || int64(*rtCost) > remainingBudget {
		return "", -1, &cel.Error{
			Type:   cel.ErrorTypeInvalid,
			Detail: "messageExpression evaluation failed due to running out of cost budget, no further validation rules will be run",
		}
	}
	if err != nil {
		if strings.HasPrefix(err.Error(), "operation cancelled: actual cost limit exceeded") {
			return "", -1, &cel.Error{
				Type:   cel.ErrorTypeInvalid,
				Detail: fmt.Sprintf("no further validation rules will be run due to call cost exceeds limit for messageExpression: %q", exprSrc),
			}
		}
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: fmt.Sprintf("messageExpression evaluation failed due to: %v", err.Error()),
		}
	}
	messageStr, ok := evalResult.Value().(string)
	if !ok {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: "messageExpression failed to convert to string",
		}
	}
	trimmedMsgStr := strings.TrimSpace(messageStr)
	if len(trimmedMsgStr) > celconfig.MaxEvaluatedMessageExpressionSizeBytes {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: fmt.Sprintf("messageExpression beyond allowable length of %d", celconfig.MaxEvaluatedMessageExpressionSizeBytes),
		}
	} else if hasNewlines(trimmedMsgStr) {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: "messageExpression should not contain line breaks",
		}
	} else if len(trimmedMsgStr) == 0 {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: "messageExpression should evaluate to a non-empty string",
		}
	}
	return trimmedMsgStr, remainingBudget - int64(*rtCost), nil
}

var newlineMatcher = regexp.MustCompile(`[\n]+`)

func hasNewlines(s string) bool {
	return newlineMatcher.MatchString(s)
}

func ruleMessageOrDefault(rule common.ValidationRule) string {
	if len(rule.Message()) == 0 {
		return fmt.Sprintf("failed rule: %s", ruleErrorString(rule))
	} else {
		return strings.TrimSpace(rule.Message())
	}
}

func ruleErrorString(rule common.ValidationRule) string {
	if len(rule.Message()) > 0 {
		return strings.TrimSpace(rule.Message())
	}
	return strings.TrimSpace(rule.Rule())
}

type validationActivation struct {
	self, oldSelf ref.Val
	hasOldSelf    bool
}

func validationActivationWithOldSelf(sts common.Schema, obj, oldObj interface{}) interpreter.Activation {
	va := &validationActivation{
		self: common.UnstructuredToVal(obj, sts),
	}
	if oldObj != nil {
		va.oldSelf = common.UnstructuredToVal(oldObj, sts) // +k8s:verify-mutation:reason=clone
		va.hasOldSelf = true                               // +k8s:verify-mutation:reason=clone
	}
	return va
}

func validationActivationWithoutOldSelf(sts common.Schema, obj, _ interface{}) interpreter.Activation {
	return &validationActivation{
		self: common.UnstructuredToVal(obj, sts),
	}
}

func (a *validationActivation) ResolveName(name string) (interface{}, bool) {
	switch name {
	case ScopedVarName:
		return a.self, true
	case OldScopedVarName:
		return a.oldSelf, a.hasOldSelf
	default:
		return nil, false
	}
}

func (a *validationActivation) Parent() interpreter.Activation {
	return nil
}

func (s *Validator) validateMap(ctx context.Context, fldPath *field.Path, obj, oldObj map[string]interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	remainingBudget = costBudget
	if remainingBudget < 0 {
		return errs, remainingBudget
	}
	if s.schema == nil || obj == nil {
		return nil, remainingBudget
	}

	correlatable := MapIsCorrelatable(s.schema.XMapType())

	aProps := s.schema.AdditionalProperties()
	if s.AdditionalProperties != nil && aProps != nil && aProps.Schema() != nil {
		for k, v := range obj {
			var oldV interface{}
			if correlatable {
				oldV = oldObj[k] // +k8s:verify-mutation:reason=clone
			}

			var err field.ErrorList
			err, remainingBudget = s.AdditionalProperties.Validate(ctx, fldPath.Key(k), v, oldV, remainingBudget)
			errs = append(errs, err...)
			if remainingBudget < 0 {
				return errs, remainingBudget
			}
		}
	}

	props := s.schema.Properties()
	if s.Properties != nil && props != nil {
		for k, v := range obj {
			sub, ok := s.Properties[k]
			if ok {
				var oldV interface{}
				if correlatable {
					oldV = oldObj[k] // +k8s:verify-mutation:reason=clone
				}

				var err field.ErrorList
				err, remainingBudget = sub.Validate(ctx, fldPath.Child(k), v, oldV, remainingBudget)
				errs = append(errs, err...)
				if remainingBudget < 0 {
					return errs, remainingBudget
				}
			}
		}
	}

	return errs, remainingBudget
}

func (s *Validator) validateArray(ctx context.Context, fldPath *field.Path, sts common.Schema, obj, oldObj []interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	remainingBudget = costBudget
	if remainingBudget < 0 {
		return errs, remainingBudget
	}

	items := sts.Items()
	if s.Items != nil && items != nil {
		// only map-type lists support self-oldSelf correlation for cel rules. if this isn't a
		// map-type list, then makeMapList returns an implementation that always returns nil
		correlatableOldItems := common.MakeMapList(sts, oldObj)
		for i := range obj {
			var err field.ErrorList
			err, remainingBudget = s.Items.Validate(ctx, fldPath.Index(i), obj[i], correlatableOldItems.Get(obj[i]), remainingBudget)
			errs = append(errs, err...)
			if remainingBudget < 0 {
				return errs, remainingBudget
			}
		}
	}

	return errs, remainingBudget
}

// MapIsCorrelatable returns true if the mapType can be used to correlate the data elements of a map after an update
// with the data elements of the map from before the updated.
func MapIsCorrelatable(mapType string) bool {
	// if a third map type is introduced, assume it's not correlatable. granular is the default if unspecified.
	return mapType == "" || mapType == "granular" || mapType == "atomic"
}

func hasXValidations(s common.Schema) bool {
	if s == nil {
		return false
	}
	if len(s.XValidations()) > 0 {
		return true
	}
	if hasXValidations(s.Items()) {
		return true
	}
	if s.AdditionalProperties() != nil && hasXValidations(s.AdditionalProperties().Schema()) {
		return true
	}
	if s.Properties() != nil {
		for _, prop := range s.Properties() {
			if hasXValidations(prop) {
				return true
			}
		}
	}
	return false
}
