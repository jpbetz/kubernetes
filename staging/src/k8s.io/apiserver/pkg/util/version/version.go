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

package version

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/pflag"

	util "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	baseversion "k8s.io/component-base/version"
)

var Effective MutableEffectiveVersions = newEffectiveVersion()

type EffectiveVersions interface {
	BinaryVersion() *version.Version
	EmulationVersion() *version.Version // TODO: remove
	EmulationVersionFor(component string) *version.Version
	MinCompatibilityVersion() *version.Version // TODO: remove
	MinCompatibilityVersionFor(component string) *version.Version
}

type MutableEffectiveVersions interface {
	EffectiveVersions
	// SetBinaryVersionForTests updates the binaryVersion.
	// Should only be used in tests.
	SetBinaryVersionForTests(binaryVersion *version.Version) func()
	Set(binaryVersion, emulationVersion, minCompatibilityVersion *version.Version) // TODO: remove
	SetBinaryVersion(binaryVersion *version.Version)
	SetVersionFor(component string, emulationVersion, minCompatibilityVersion *version.Version)
	AddFlags(fs *pflag.FlagSet)
	Validate() []error
}

type VersionVar struct {
	Val atomic.Pointer[version.Version]
}

// Set sets the flag value
func (v *VersionVar) Set(s string) error {
	components := strings.Split(s, ".")
	if len(components) != 2 {
		return fmt.Errorf("version %s is not in the format of major.minor", s)
	}
	ver, err := version.ParseGeneric(s)
	if err != nil {
		return err
	}
	v.Val.Store(ver)
	return nil
}

// String returns the flag value
func (v *VersionVar) String() string {
	ver := v.Val.Load()
	return ver.String()
}

// Type gets the flag type
func (v *VersionVar) Type() string {
	return "version"
}

type effectiveVersions struct {
	binaryVersion atomic.Pointer[version.Version]

	// TODO: godoc
	// componentVersions is a map of component name to componentEffectiveVersions
	componentVersions sync.Map
}

func (m *effectiveVersions) BinaryVersion() *version.Version {
	return m.binaryVersion.Load()
}

func (m *effectiveVersions) getComponentVersions(component string) *componentEffectiveVersions {
	v, ok := m.componentVersions.Load(component)
	if !ok {
		return nil
	}
	cv, ok := v.(*componentEffectiveVersions)
	if !ok {
		util.HandleError(fmt.Errorf("expected componentEffectiveVersions but got %T", v))
		return nil
	}
	return cv
}

func (m *effectiveVersions) setComponentVersions(component string, effectiveVersions *componentEffectiveVersions) {
	m.componentVersions.Store(component, effectiveVersions)
}

type componentEffectiveVersions struct {
	// If the emulationVersion is set by the users, it could only contain major and minor versions.
	// In tests, emulationVersion could be the same as the binary version, or set directly,
	// which can have "alpha" as pre-release to continue serving expired apis while we clean up the test.
	emulationVersion VersionVar
	// minCompatibilityVersion could only contain major and minor versions.
	minCompatibilityVersion VersionVar
}

func (m *effectiveVersions) EmulationVersion() *version.Version {
	// TODO: remove
	return nil
}

// TODO: Godoc
func (m *effectiveVersions) EmulationVersionFor(component string) *version.Version {
	// Emulation version can have "alpha" as pre-release to continue serving expired apis while we clean up the test.
	// The pre-release should not be accessible to the users.

	cv := m.getComponentVersions(component)
	if cv == nil {
		return nil
	}
	return cv.emulationVersion.Val.Load().WithPreRelease(m.BinaryVersion().PreRelease())
}

func (m *effectiveVersions) MinCompatibilityVersion() *version.Version {
	// TODO: remove
	return nil
}

// TODO: Godoc
func (m *effectiveVersions) MinCompatibilityVersionFor(component string) *version.Version {
	cv := m.getComponentVersions(component)
	if cv == nil {
		return nil
	}
	return cv.minCompatibilityVersion.Val.Load()
}

func (m *effectiveVersions) Set(binaryVersion, emulationVersion, minCompatibilityVersion *version.Version) {
	// TODO: remove
}

func (m *effectiveVersions) SetBinaryVersion(binaryVersion *version.Version) {
	m.binaryVersion.Store(binaryVersion)
}

// TODO: Godoc
func (m *effectiveVersions) SetVersionFor(component string, emulationVersion, minCompatibilityVersion *version.Version) {
	cv := m.getComponentVersions(component)
	if cv == nil {
		cv = &componentEffectiveVersions{}
		m.setComponentVersions(component, cv)
	}
	cv.emulationVersion.Val.Store(emulationVersion)
	cv.minCompatibilityVersion.Val.Store(minCompatibilityVersion)
}

func (m *effectiveVersions) SetBinaryVersionForTests(binaryVersion *version.Version) func() {
	oldBinaryVersion := m.binaryVersion.Load()
	m.SetBinaryVersion(binaryVersion)
	m.SetVersionFor("k8s.io/apiserver", binaryVersion, version.MajorMinor(binaryVersion.Major(), binaryVersion.SubtractMinor(1).Minor()))
	oldFeatureGateVersion := utilfeature.DefaultVersionedFeatureGate.EmulationVersion()
	if err := utilfeature.DefaultMutableVersionedFeatureGate.SetEmulationVersion(binaryVersion); err != nil {
		panic(err)
	}
	return func() {
		m.SetBinaryVersion(oldBinaryVersion)
		m.SetVersionFor("k8s.io/apiserver", oldBinaryVersion, version.MajorMinor(oldBinaryVersion.Major(), oldBinaryVersion.SubtractMinor(1).Minor()))
		utilfeature.DefaultMutableVersionedFeatureGate.(featuregate.MutableVersionedFeatureGateForTests).Reset()
		if err := utilfeature.DefaultMutableVersionedFeatureGate.SetEmulationVersion(oldFeatureGateVersion); err != nil {
			panic(err)
		}
	}
}

func (m *effectiveVersions) Validate() []error {
	var errs []error
	// Validate only checks the major and minor versions.
	binaryVersion := m.binaryVersion.Load().WithPatch(0)
	emulationVersion := m.emulationVersion.Val.Load().WithPatch(0)
	minCompatibilityVersion := m.minCompatibilityVersion.Val.Load().WithPatch(0)

	maxEmuVer := binaryVersion
	minEmuVer := binaryVersion
	// emulationVersion can only be 1.{binaryMinor-1}...1.{binaryMinor} for alpha if EmulationVersion feature gate is enabled.
	// otherwise, emulationVersion can only be 1.{binaryMinor}.
	if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.EmulationVersion) {
		minEmuVer = binaryVersion.SubtractMinor(1)
		// emulationVersion concept is introduced in 1.30, it cannot be set to be less than that.
		if minEmuVer.LessThan(version.MajorMinor(1, 30)) {
			minEmuVer = version.MajorMinor(1, 30)
		}
	}
	if emulationVersion.GreaterThan(maxEmuVer) || emulationVersion.LessThan(minEmuVer) {
		errs = append(errs, fmt.Errorf("emulation version %s is not between [%s, %s]", emulationVersion.String(), minEmuVer.String(), maxEmuVer.String()))
	}
	// minCompatibilityVersion can only be 1.{binaryMinor-1} for alpha.
	maxCompVer := binaryVersion.SubtractMinor(1)
	minCompVer := binaryVersion.SubtractMinor(1)
	if minCompatibilityVersion.GreaterThan(maxCompVer) || minCompatibilityVersion.LessThan(minCompVer) {
		errs = append(errs, fmt.Errorf("minCompatibilityVersion version %s is not between [%s, %s]", minCompatibilityVersion.String(), minCompVer.String(), maxCompVer.String()))
	}
	return errs
}

func (m *effectiveVersions) AddFlags(fs *pflag.FlagSet) {
	if m == nil {
		return
	}

	fs.Var(&m.emulationVersion, "emulated-version", ""+
		"The version the K8s component emulates its capabilities (APIs, features, ...) of.\n"+
		"If set, the component would behave like the set version instead of the underlying binary version.\n"+
		"Any capabilities present in the binary version that were introduced after the emulated version will be unavailable and any capabilities removed after the emulated version will be available.\n"+
		"Defaults to the binary version. The value should be between 1.{binaryMinorVersion-1} and 1.{binaryMinorVersion}.\n"+
		"Format could only be major.minor")
}

func newEffectiveVersion() MutableEffectiveVersions {
	effective := &effectiveVersions{}
	binaryVersionInfo := baseversion.Get()
	binaryVersion := version.MustParse(binaryVersionInfo.String())
	// compatVersion := version.MajorMinor(binaryVersion.Major(), binaryVersion.SubtractMinor(1).Minor())
	effective.SetBinaryVersion(binaryVersion)
	// TODO: apiserver configuration should allow for binary version -> apiserver component version mapping
	// effective.SetVersionFor("k8s.io/kube-apiserver", binaryVersion, compatVersion)
	return effective
}
