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
)

// TODO: Parse path from string?  There are tools for this...

type Path []Part

type Part struct {
	Field string
	Index int
	Key   string
}

func Tweak(in any, path Path, newValue any) error {
	rv := reflect.ValueOf(in)
	for _, part := range path {
		for rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if len(part.Field) > 0 {
			rv = rv.FieldByName(part.Field)
			if rv.IsZero() {
				return fmt.Errorf("field %q not found", part.Field)
			}
		}
	}
	rv.Set(reflect.ValueOf(newValue))
	return nil
}
