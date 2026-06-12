/*
Copyright 2026 The Kubernetes Authors.
*/

package generators

import (
	"strings"
	"testing"

	"k8s.io/code-generator/cmd/conversion-gen/args"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

func makePkg(path string, comments []string, typeNames []string, typeComments map[string][]string, members map[string][]types.Member) *types.Package {
	pkg := &types.Package{
		Path:     path,
		Name:     path,
		Comments: comments,
		Types:    map[string]*types.Type{},
	}
	for _, tname := range typeNames {
		var m []types.Member
		if members != nil {
			m = members[tname]
		}
		var tc []string
		if typeComments != nil {
			tc = typeComments[tname]
		}
		pkg.Types[tname] = &types.Type{
			Name:         types.Name{Package: path, Name: tname},
			Kind:         types.Struct,
			CommentLines: tc,
			Members:      m,
		}
	}
	return pkg
}

func TestValidateGroupHubs(t *testing.T) {
	cases := []struct {
		name          string
		pkgs          []*types.Package
		inputs        []string
		pkgToPeers    map[string][]string
		pkgToExternal map[string]string
		requireHub    bool
		expectedError string
	}{
		{
			name: "success: 1 hub, memory identical",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/internal", []string{"+groupName=testgroup.k8s.io"}, []string{"TypeA"}, nil, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.Int32}},
				}),
				makePkg("example.com/pkg/v1", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, map[string][]string{
					"TypeA": {"+k8s:conversion-hub"},
				}, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.Int32}},
				}),
				makePkg("example.com/pkg/v2", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.String}}, // Different, but not hub
				}),
			},
			inputs: []string{"example.com/pkg/v1", "example.com/pkg/v2"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
				"example.com/pkg/v2": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
				"example.com/pkg/v2": "example.com/pkg/v2",
			},
			requireHub:    false,
			expectedError: "",
		},
		{
			name: "success: groupName from peer fallback",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/internal", []string{"+groupName=testgroup.k8s.io"}, []string{"TypeA"}, nil, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.Int32}},
				}),
				makePkg("example.com/pkg/v1", []string{"+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, map[string][]string{
					"TypeA": {"+k8s:conversion-hub"},
				}, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.Int32}},
				}),
				makePkg("example.com/pkg/v2", []string{"+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.String}},
				}),
			},
			inputs: []string{"example.com/pkg/v1", "example.com/pkg/v2"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
				"example.com/pkg/v2": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
				"example.com/pkg/v2": "example.com/pkg/v2",
			},
			requireHub:    true, // Enforce it to ensure the group is identified
			expectedError: "",
		},
		{
			name: "failure: multiple hubs",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/v1", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, map[string][]string{
					"TypeA": {"+k8s:conversion-hub"},
				}, nil),
				makePkg("example.com/pkg/v2", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, map[string][]string{
					"TypeA": {"+k8s:conversion-hub"},
				}, nil),
			},
			inputs: []string{"example.com/pkg/v1", "example.com/pkg/v2"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
				"example.com/pkg/v2": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
				"example.com/pkg/v2": "example.com/pkg/v2",
			},
			requireHub:    false,
			expectedError: `multiple conversion-hubs`,
		},
		{
			name: "failure: hub not memory identical",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/internal", []string{"+groupName=testgroup.k8s.io"}, []string{"TypeA"}, nil, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.Int32}},
				}),
				makePkg("example.com/pkg/v1", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, map[string][]string{
					"TypeA": {"+k8s:conversion-hub"},
				}, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.String}}, // Different type
				}),
			},
			inputs: []string{"example.com/pkg/v1"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
			},
			requireHub:    false,
			expectedError: `is not memory-identical to internal type`,
		},
		{
			name: "success: internal type opted out (RequireConversionHub=true)",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/internal", []string{"+groupName=testgroup.k8s.io"}, []string{"TypeA"}, map[string][]string{
					"TypeA": {"+k8s:conversion-hub=false"}, // Opt-out on internal type!
				}, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.Int32}},
				}),
				makePkg("example.com/pkg/v1", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, map[string][]types.Member{
					"TypeA": {{Name: "X", Type: types.String}}, // Different, and not hub
				}),
			},
			inputs: []string{"example.com/pkg/v1"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
			},
			requireHub:    true,
			expectedError: "",
		},
		{
			name: "failure: 0 hubs (RequireConversionHub=true)",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/v1", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, nil),
				makePkg("example.com/pkg/v2", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, nil),
				makePkg("example.com/pkg/internal", []string{"+groupName=testgroup.k8s.io"}, []string{"TypeA"}, nil, nil),
			},
			inputs: []string{"example.com/pkg/v1", "example.com/pkg/v2"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
				"example.com/pkg/v2": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
				"example.com/pkg/v2": "example.com/pkg/v2",
			},
			requireHub:    true,
			expectedError: `must have a conversion-hub, but none was found`,
		},
		{
			name: "success: 0 hubs (RequireConversionHub=false)",
			pkgs: []*types.Package{
				makePkg("example.com/pkg/v1", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, nil),
				makePkg("example.com/pkg/v2", []string{"+groupName=testgroup.k8s.io", "+k8s:conversion-gen=example.com/pkg/internal"}, []string{"TypeA"}, nil, nil),
				makePkg("example.com/pkg/internal", []string{"+groupName=testgroup.k8s.io"}, []string{"TypeA"}, nil, nil),
			},
			inputs: []string{"example.com/pkg/v1", "example.com/pkg/v2"},
			pkgToPeers: map[string][]string{
				"example.com/pkg/v1": {"example.com/pkg/internal"},
				"example.com/pkg/v2": {"example.com/pkg/internal"},
			},
			pkgToExternal: map[string]string{
				"example.com/pkg/v1": "example.com/pkg/v1",
				"example.com/pkg/v2": "example.com/pkg/v2",
			},
			requireHub:    false,
			expectedError: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			universe := types.Universe{}
			for _, pkg := range tc.pkgs {
				universe[pkg.Path] = pkg
			}
			context := &generator.Context{
				Universe: universe,
			}
			generatorArgs := &args.Args{
				RequireConversionHub: tc.requireHub,
			}

			err := validateGroupHubs(context, generatorArgs, tc.inputs, tc.pkgToPeers, tc.pkgToExternal)
			if tc.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.expectedError)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatalf("expected error containing %q, got: %v", tc.expectedError, err)
				}
			}
		})
	}
}
