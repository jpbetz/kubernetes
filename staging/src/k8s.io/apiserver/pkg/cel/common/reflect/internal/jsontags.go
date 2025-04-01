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

package internal

import (
	"reflect"
	"strings"
)

// TODO: Below is copy pasted from gengo and modified so that LookupJSON accepts a reflect.StructField.
//       This needs a better home.

// JSON represents a go json jsonTag tag.
type JSON struct {
	Name      string
	Omit      bool
	Inline    bool
	Omitempty bool
}

func (t JSON) String() string {
	var tag string
	if !t.Inline {
		tag += t.Name
	}
	if t.Omitempty {
		tag += ",omitempty"
	}
	if t.Inline {
		tag += ",inline"
	}
	return tag
}

func LookupJSON(m reflect.StructField) (JSON, bool) {
	tag := m.Tag.Get("json")
	if tag == "" {
		return JSON{}, false
	}
	if tag == "-" {
		return JSON{Omit: true}, true
	}
	name, opts := parse(tag)
	inline := opts.Contains("inline")
	omitempty := opts.Contains("omitempty")
	if !inline && name == "" {
		name = m.Name
	}
	return JSON{
		Name:      name,
		Omit:      false,
		Inline:    inline,
		Omitempty: omitempty,
	}, true
}

type options string

// parse splits a struct field's json tag into its name and
// comma-separated options.
func parse(tag string) (string, options) {
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx], options(tag[idx+1:])
	}
	return tag, ""
}

// Contains reports whether a comma-separated listAlias of options
// contains a particular substr flag. substr must be surrounded by a
// string boundary or commas.
func (o options) Contains(optionName string) bool {
	if len(o) == 0 {
		return false
	}
	s := string(o)
	for s != "" {
		var next string
		i := strings.Index(s, ",")
		if i >= 0 {
			s, next = s[:i], s[i+1:]
		}
		if s == optionName {
			return true
		}
		s = next
	}
	return false
}
