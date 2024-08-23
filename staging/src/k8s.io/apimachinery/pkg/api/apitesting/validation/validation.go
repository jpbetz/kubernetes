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

package validation

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"text/scanner"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// MustSet returns a copy of obj where the value a path is replaced with the given value.
// The path is a field path from the root of obj to the value to set.
// The field path may contain `.<field>`, `[<key>]` and `[<index>]` accessors as well as `[<key1>:<value1>,...,<keyN>:<valueN>]`
// selectors for +listType=map slices.
//
// Example paths:
//
//   - spec.containers[0].image
//   - spec.containers[name:nginx].ports[containerPort:80,protocol:TCP].hostPort
//   - status.conditions[type:Resizing]
//   - spec.metadata.label["env"]
func MustSet[T runtime.Object](obj T, path string, value any) T {
	result, err := Set(obj, path, value)
	if err != nil {
		panic(fmt.Sprintf("MustSet error: %v", err))
	}
	return result
}

// ExpectErrors compares an actual list of field errors against an expected list of errors matchers.
// If any actual errors are unmatched, or if any of the expected matches are not found, test errors
// are reported to the given test state t.
func ExpectErrors(t *testing.T, actual field.ErrorList, expected []ErrorMatcher) {
	notMatched, expectedNotFound := MatchErrors(actual, expected)
	for _, nm := range notMatched {
		t.Errorf("Unexpected error: %v", nm)
	}
	for _, exp := range expectedNotFound {
		t.Errorf("Expected error matching: %v", exp)
	}
}

// ErrorMatcher
type ErrorMatcher struct {
	// Type is required when matching errors
	Type field.ErrorType

	// Field is an optional matcher for the field path of the error. If set, the error field path much match exactly.
	Field *string

	// FieldContains is an optional matcher for the field path of the error. If set, the error field path much contain this string.
	FieldContains *string

	// Detail is an optional matcher for the detail message of the error. If set, the error detail message much match exactly.
	Detail *string
	// DetailContains is an optional matcher for the detail message of the error. If set, the error detail message much contain this string.
	DetailContains *string

	// BadValue is an optional matcher for the bad value of the error. If set, the error bad value must be equal.  Equality is
	// checked with equality.Semantic.DeepEqual.
	BadValue *any
}

func (e ErrorMatcher) Matches(err *field.Error) bool {
	if e.Type != err.Type {
		return false
	}
	if e.Field != nil && *e.Field != err.Field {
		return false
	}
	// TODO: Contains might not be enough here..
	if e.FieldContains != nil && !strings.Contains(err.Field, *e.FieldContains) {
		return false
	}
	if e.Detail != nil && *e.Detail != err.Detail {
		return false
	}
	// TODO: Contains might not be enough here..
	if e.DetailContains != nil && !strings.Contains(err.Detail, *e.DetailContains) {
		return false
	}
	if e.BadValue != nil && !equality.Semantic.DeepEqual(*e.BadValue, err.BadValue) {
		return false
	}
	return true
}

func (e ErrorMatcher) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("Type: %v", e.Type))
	if e.Field != nil {
		sb.WriteString(fmt.Sprintf(", Field: %v", *e.Field))
	}
	if e.FieldContains != nil {
		sb.WriteString(fmt.Sprintf(", FieldContains: %v", *e.FieldContains))
	}
	if e.Detail != nil {
		sb.WriteString(fmt.Sprintf(", Detail: %v", e.Detail))
	}
	if e.DetailContains != nil {
		sb.WriteString(fmt.Sprintf(", DetailContains: %v", e.DetailContains))
	}
	if e.BadValue != nil {
		sb.WriteString(fmt.Sprintf(", BadValue: %v", e.BadValue))
	}
	return sb.String()
}

func MatchErrors(actual field.ErrorList, expected []ErrorMatcher) (notMatched []*field.Error, expectedNotFound []ErrorMatcher) {
	expectedNotFound = make([]ErrorMatcher, 0, len(expected))
	copy(expected, expectedNotFound)

	for _, a := range actual {
		for i, u := range expectedNotFound {
			if u.Matches(a) {
				expectedNotFound = append(expectedNotFound[:i], expectedNotFound[i+1:]...)
			} else {
				notMatched = append(notMatched, a)
			}
		}
	}
	return notMatched, expectedNotFound
}

func Set[T runtime.Object](obj T, path string, value any) (T, error) {
	p, err := parsePath(path)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to parse path: %v", err)
	}

	result := obj.DeepCopyObject().(T)
	err = setAtPath(result, p, value)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to modify object: %v", err)
	}
	return result, nil
}

type path []part

func (p path) String() string {
	b := strings.Builder{}
	for i, part := range p {
		if part.Index != nil {
			b.WriteString("[")
			b.WriteString(strconv.Itoa(*part.Index))
			b.WriteString("]")
		} else if part.Key != nil {
			b.WriteString(*part.Key)
			if i < len(p)-1 {
				b.WriteString(".")
			}
		} else if len(part.ListMapKey) > 0 {
			b.WriteString("[")
			for k, v := range part.ListMapKey {
				b.WriteString(k)
				b.WriteString(":")
				b.WriteString(fmt.Sprintf("%v", v))
			}
			if i < len(part.ListMapKey)-1 {
				b.WriteString(",")
			}
			b.WriteString("]")
		}
	}
	return b.String()
}

type part struct {
	Index      *int
	Key        *string
	ListMapKey map[string]any
}

func parsePath(in string) (path, error) {
	result := path{}

	var s scanner.Scanner
	s.Mode ^= scanner.SkipComments // disallow comments
	s.Init(strings.NewReader(in))

	var errs []string
	s.Error = func(scanner *scanner.Scanner, msg string) {
		errs = append(errs, fmt.Errorf("error parsing '%s' at %v: %s", in, scanner.Position, msg).Error())
	}
	unexpectedTokenError := func(expected string, token string) (path, error) {
		s.Error(&s, fmt.Sprintf("expected %s but got (%q)", expected, token))
		return nil, fmt.Errorf(strings.Join(errs, ", "))
	}

	for {
		token := s.Scan()
		switch token {
		case scanner.Ident:
			// Only allow field name without leading `.` at start of path.
			if len(result) > 0 {
				return unexpectedTokenError(". or [", s.TokenText())
			}
			key := s.TokenText()
			result = append(result, part{Key: &key})
		case '.':
			if s.Scan() != scanner.Ident {
				return unexpectedTokenError("field name", s.TokenText())
			}
			key := s.TokenText()
			result = append(result, part{Key: &key})
		case '[':
			keyOrIndex := s.Scan()
			switch keyOrIndex {
			case scanner.String, scanner.RawString:
				key, err := strconv.Unquote(s.TokenText())
				if err != nil {
					return unexpectedTokenError("map key", s.TokenText())
				}
				result = append(result, part{Key: &key})
			case scanner.Int:
				index, err := strconv.Atoi(s.TokenText())
				if err != nil {
					return unexpectedTokenError("array index", s.TokenText())
				}
				result = append(result, part{Index: &index})
			case scanner.Ident:
				if s.Peek() == ']' {
					key := s.TokenText()
					result = append(result, part{Key: &key})
				} else {
					mapListKey, err := parseListMapKey(&s, token, unexpectedTokenError)
					if err != nil {
						return nil, err
					}
					result = append(result, part{ListMapKey: mapListKey})
				}
			default:
				return unexpectedTokenError("field value", s.TokenText())
			}
			if s.Scan() != ']' {
				return unexpectedTokenError("]", s.TokenText())
			}
		case scanner.EOF:
			return result, nil
		default:
			return unexpectedTokenError("path part", s.TokenText())
		}
	}
}

func parseListMapKey(s *scanner.Scanner, token rune, unexpectedTokenError func(expected string, token string) (path, error)) (map[string]any, error) {
	listMapKey := map[string]any{}
	keyField := s.TokenText()

	for {
		if s.Scan() != ':' {
			_, err := unexpectedTokenError(":", s.TokenText())
			return nil, err
		}

		var value any
		var err error
		switch s.Scan() {
		case scanner.String, scanner.RawString:
			value, err = strconv.Unquote(s.TokenText())
			if err != nil {
				_, err := unexpectedTokenError("string", s.TokenText())
				return nil, err
			}
		case scanner.Int:
			value, err = strconv.Atoi(s.TokenText())
			if err != nil {
				_, err := unexpectedTokenError("number", s.TokenText())
				return nil, err
			}
		case scanner.Ident:
			token := s.TokenText()
			if token == "true" {
				value = true
			} else if token == "false" {
				value = false
			} else {
				value = token
			}
		default:
			_, err := unexpectedTokenError("key value", s.TokenText())
			return nil, err
		}
		listMapKey[keyField] = value
		switch s.Peek() {
		case ']':
			return listMapKey, nil
		case ',':
			s.Next()
			if s.Scan() != scanner.Ident {
				_, err := unexpectedTokenError("map key part field name", s.TokenText())
				return nil, err
			}
			keyField = s.TokenText()

		default:
			_, err := unexpectedTokenError(", or ]", s.TokenText())
			return nil, err
		}
	}
}

func setAtPath(in any, path path, newValue any) error {
	rv := reflect.ValueOf(in)
	for _, part := range path {
		for rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if part.Key != nil {
			key := *part.Key
			switch rv.Kind() {
			case reflect.Map:
				rv = rv.MapIndex(reflect.ValueOf(key))
				if !rv.IsValid() {
					return fmt.Errorf("map key %q not found", key)
				}
			case reflect.Struct:
				var ok bool
				rv, ok = lookupField(rv, key)
				if !ok || !rv.IsValid() {
					return fmt.Errorf("struct field %q not found", key)
				}
			default:
				return fmt.Errorf("expected struct or map but got %v", rv.Kind())
			}
		} else if part.Index != nil {
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				rv = rv.Index(*part.Index)
				if !rv.IsValid() {
					return fmt.Errorf("slice or array index '%d' not found", *part.Index)
				}
			default:
				return fmt.Errorf("expected struct or map but got %v", rv.Kind())
			}
		} else if part.ListMapKey != nil {
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
			default:
				return fmt.Errorf("expected slice but got %v", rv.Kind())
			}
			found := false
		outer:
			for i := 0; i < rv.Len(); i++ {
				c := rv.Index(i)
				for c.Kind() == reflect.Ptr {
					c = c.Elem()
				}
				if c.Kind() != reflect.Struct {
					return fmt.Errorf("list element %q is not a struct", i)
				}
				for k, v := range part.ListMapKey {
					cv, ok := lookupField(c, k)
					if !ok || !reflect.ValueOf(v).Equal(cv) {
						continue outer
					}
				}
				rv = c
				found = true
				break
			}
			if !found {
				return fmt.Errorf("list map entry %q not found", part.ListMapKey)
			}

		}
	}
	if !rv.IsValid() {
		return fmt.Errorf("value not found for path %q", path)
	}
	if !rv.CanAddr() {
		return fmt.Errorf("value '%v' at path %q cannot be set since it cannot be addressed, consider setting a parent value instead", rv, path)
	}
	rv.Set(reflect.ValueOf(newValue))
	return nil
}

func lookupField(structVal reflect.Value, name string) (reflect.Value, bool) {
	for i := 0; i < structVal.NumField(); i++ {
		f := structVal.Type().Field(i)
		jsonName := parseTag(f.Tag.Get("json"))
		if name == jsonName || name == f.Name {
			return structVal.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// parseTag splits a struct field's json tag into its name and
// comma-separated options.
func parseTag(tag string) string {
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx]
	}
	return tag
}
