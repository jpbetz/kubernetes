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

package resource

import (
	"iter"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ContainerType signifies container type
type ContainerType int

const (
	// Containers is for normal containers
	Containers ContainerType = 1 << iota
	// InitContainers is for init containers
	InitContainers
)

// PodResourcesOptions controls the behavior of PodRequests and PodLimits.
type PodResourcesOptions struct {
	// Reuse, if provided will be reused to accumulate resources and returned by the PodRequests or PodLimits
	// functions. All existing values in Reuse will be lost.
	Reuse v1.ResourceList
	// UseStatusResources indicates whether resources reported by the PodStatus should be considered
	// when evaluating the pod resources. This MUST be false if the InPlacePodVerticalScaling
	// feature is not enabled.
	UseStatusResources bool
	// ExcludeOverhead controls if pod overhead is excluded from the calculation.
	ExcludeOverhead bool
	// ContainerFn is called with the effective resources required for each container within the pod.
	ContainerFn func(res v1.ResourceList, containerType ContainerType)
	// NonMissingContainerRequests if provided will replace any missing container level requests for the specified resources
	// with the given values.  If the requests for those resources are explicitly set, even if zero, they will not be modified.
	NonMissingContainerRequests v1.ResourceList
	// SkipPodLevelResources controls whether pod-level resources should be skipped
	// from the calculation. If pod-level resources are not set in PodSpec,
	// pod-level resources will always be skipped.
	SkipPodLevelResources bool
}

var supportedPodLevelResources = sets.New(v1.ResourceCPU, v1.ResourceMemory)

func SupportedPodLevelResources() sets.Set[v1.ResourceName] {
	return supportedPodLevelResources
}

// IsSupportedPodLevelResources checks if a given resource is supported by pod-level
// resource management through the PodLevelResources feature. Returns true if
// the resource is supported.
func IsSupportedPodLevelResource(name v1.ResourceName) bool {
	return supportedPodLevelResources.Has(name)
}

// IsPodLevelResourcesSet check if PodLevelResources pod-level resources are set.
// It returns true if either the Requests or Limits maps are non-empty.
func IsPodLevelResourcesSet(pod *v1.Pod) bool {
	if pod.Spec.Resources == nil {
		return false
	}

	if (len(pod.Spec.Resources.Requests) + len(pod.Spec.Resources.Limits)) == 0 {
		return false
	}

	for resourceName := range pod.Spec.Resources.Requests {
		if IsSupportedPodLevelResource(resourceName) {
			return true
		}
	}

	for resourceName := range pod.Spec.Resources.Limits {
		if IsSupportedPodLevelResource(resourceName) {
			return true
		}
	}

	return false
}

// IsPodLevelRequestsSet checks if pod-level requests are set. It returns true if
// Requests map is non-empty.
func IsPodLevelRequestsSet(pod *v1.Pod) bool {
	if pod.Spec.Resources == nil {
		return false
	}

	if len(pod.Spec.Resources.Requests) == 0 {
		return false
	}

	for resourceName := range pod.Spec.Resources.Requests {
		if IsSupportedPodLevelResource(resourceName) {
			return true
		}
	}

	return false
}

// PodRequests computes the total pod requests per the PodResourcesOptions supplied.
// If PodResourcesOptions is nil, then the requests are returned including pod overhead.
// If the PodLevelResources feature is enabled AND the pod-level resources are set,
// those pod-level values are used in calculating Pod Requests.
// The computation is part of the API and must be reviewed as an API change.
func PodRequests(pod *v1.Pod, opts PodResourcesOptions) v1.ResourceList {
	reqs := AggregateContainerRequests(pod, opts)
	if !opts.SkipPodLevelResources && IsPodLevelRequestsSet(pod) {
		for resourceName, quantity := range pod.Spec.Resources.Requests {
			if IsSupportedPodLevelResource(resourceName) {
				reqs[resourceName] = quantity
			}
		}
	}

	// Add overhead for running a pod to the sum of requests if requested:
	if !opts.ExcludeOverhead && pod.Spec.Overhead != nil {
		addResourceList(reqs, pod.Spec.Overhead)
	}

	return reqs
}

// PodSpecContainerResources provides access to the container resource requests in a pod spec.
type PodSpecContainerResources interface {
	// Containers iterates over the name and ContainerResources of the containers in the pod spec.
	Containers() iter.Seq2[string, ContainerResources]
	// InitContainers iterates over the name and ContainerResources of the init containers in the pod spec.
	InitContainers() iter.Seq2[string, InitContainerResources]
}

// PodStatusContainerResources provides access to the container resource requests in a pod status.
type PodStatusContainerResources interface {
	// ContainerStatuses iterates over the name and ContainerResources of container statuses in the pod status.
	ContainerStatuses() iter.Seq2[string, ContainerResources]
	// IsPodResizeStatusInfeasible returns true if the pod resize status is infeasible.
	IsPodResizeStatusInfeasible() bool
}

// ContainerResources provides access to the request resources of a container.
type ContainerResources interface {
	// GetRequests returns the container's resource requests.
	GetRequests() v1.ResourceList
}

// InitContainerResources provides access to the request resources of an init container.
type InitContainerResources interface {
	ContainerResources
	// IsContainerRestartPolicyAlways returns true if the init container has a restart policy of Always.
	IsContainerRestartPolicyAlways() bool
}

// AggregatePodContainerRequests computes the total resource requests of all the containers
// in a pod spec and status. This computation follows the formula defined in the KEP for sidecar
// containers. See https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers#resources-calculation-for-scheduling-and-pod-admission
// for more details.
// If opts.UseStatusResources is true, a status should be provided. If UseStatusResources is false, the status is ignored
// and may be nil.
func AggregatePodContainerRequests(spec PodSpecContainerResources, status PodStatusContainerResources, opts PodResourcesOptions) v1.ResourceList {
	// attempt to reuse the maps if passed, or allocate otherwise
	reqs := reuseOrClearResourceList(opts.Reuse)
	var containerStatuses map[string]v1.ResourceList
	if opts.UseStatusResources {
		containerStatuses = make(map[string]v1.ResourceList) // FIXME: Add a length function to PodSpecContainerResources and use it to allocate the map size
		for name, request := range status.ContainerStatuses() {
			containerStatuses[name] = request.GetRequests()
		}
	}

	for name, containerResources := range spec.Containers() {
		containerReqs := containerResources.GetRequests()
		if opts.UseStatusResources {
			cs, found := containerStatuses[name]
			if found {
				if status.IsPodResizeStatusInfeasible() {
					containerReqs = cs.DeepCopy()
				} else {
					containerReqs = max(containerReqs, cs)
				}
			}
		}

		if len(opts.NonMissingContainerRequests) > 0 {
			containerReqs = applyNonMissing(containerReqs, opts.NonMissingContainerRequests)
		}

		if opts.ContainerFn != nil {
			opts.ContainerFn(containerReqs, Containers)
		}

		addResourceList(reqs, containerReqs)
	}

	restartableInitContainerReqs := v1.ResourceList{}
	initContainerReqs := v1.ResourceList{}
	// init containers define the minimum of any resource
	// Note: In-place resize is not allowed for InitContainers, so no need to check for ResizeStatus value
	//
	// Let's say `InitContainerUse(i)` is the resource requirements when the i-th
	// init container is initializing, then
	// `InitContainerUse(i) = sum(Resources of restartable init containers with index < i) + Resources of i-th init container`.
	//
	// See https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers#exposing-pod-resource-requirements for the detail.
	for _, container := range spec.InitContainers() {
		containerReqs := container.GetRequests()
		if len(opts.NonMissingContainerRequests) > 0 {
			containerReqs = applyNonMissing(containerReqs, opts.NonMissingContainerRequests)
		}

		if container.IsContainerRestartPolicyAlways() {
			// and add them to the resulting cumulative container requests
			addResourceList(reqs, containerReqs)

			// track our cumulative restartable init container resources
			addResourceList(restartableInitContainerReqs, containerReqs)
			containerReqs = restartableInitContainerReqs
		} else {
			tmp := v1.ResourceList{}
			addResourceList(tmp, containerReqs)
			addResourceList(tmp, restartableInitContainerReqs)
			containerReqs = tmp
		}

		if opts.ContainerFn != nil {
			opts.ContainerFn(containerReqs, InitContainers)
		}
		maxResourceList(initContainerReqs, containerReqs)
	}

	maxResourceList(reqs, initContainerReqs)
	return reqs
}

// AggregateContainerRequests computes the total resource requests of all the containers
// in a pod. This computation follows the formula defined in the KEP for sidecar
// containers. See https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers#resources-calculation-for-scheduling-and-pod-admission
// for more details.
func AggregateContainerRequests(pod *v1.Pod, opts PodResourcesOptions) v1.ResourceList {
	accessor := v1PodAccessor{pod} // implements both spec and status accessor interfaces
	return AggregatePodContainerRequests(accessor, accessor, opts)
}

type v1PodAccessor struct {
	*v1.Pod
}

func (p v1PodAccessor) Containers() iter.Seq2[string, ContainerResources] {
	return func(yield func(string, ContainerResources) bool) {
		for i := range p.Spec.Containers {
			yield(p.Spec.Containers[i].Name, v1PodContainerAccessor{&p.Spec.Containers[i]})
		}
	}
}

func (p v1PodAccessor) InitContainers() iter.Seq2[string, InitContainerResources] {
	return func(yield func(string, InitContainerResources) bool) {
		for i := range p.Spec.InitContainers {
			yield(p.Spec.InitContainers[i].Name, v1PodContainerAccessor{&p.Spec.InitContainers[i]})
		}
	}
}

func (p v1PodAccessor) ContainerStatuses() iter.Seq2[string, ContainerResources] {
	return func(yield func(string, ContainerResources) bool) {
		for i := range p.Status.ContainerStatuses {
			yield(p.Status.ContainerStatuses[i].Name, v1PodStatusContainerAccessor{&p.Status.ContainerStatuses[i]})
		}
	}
}

func (p v1PodAccessor) IsPodResizeStatusInfeasible() bool {
	return p.Status.Resize == v1.PodResizeStatusInfeasible
}

type v1PodContainerAccessor struct {
	*v1.Container
}

func (c v1PodContainerAccessor) GetContainerName() string {
	return c.Name
}

func (c v1PodContainerAccessor) GetRequests() v1.ResourceList {
	return c.Resources.Requests
}

func (c v1PodContainerAccessor) IsContainerRestartPolicyAlways() bool {
	return c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways
}

type v1PodStatusContainerAccessor struct {
	*v1.ContainerStatus
}

func (c v1PodStatusContainerAccessor) GetContainerName() string {
	return c.Name
}

func (c v1PodStatusContainerAccessor) GetRequests() v1.ResourceList {
	return c.Resources.Requests
}

// applyNonMissing will return a copy of the given resource list with any missing values replaced by the nonMissing values
func applyNonMissing(reqs v1.ResourceList, nonMissing v1.ResourceList) v1.ResourceList {
	cp := v1.ResourceList{}
	for k, v := range reqs {
		cp[k] = v.DeepCopy()
	}

	for k, v := range nonMissing {
		if _, found := reqs[k]; !found {
			rk := cp[k]
			rk.Add(v)
			cp[k] = rk
		}
	}
	return cp
}

// PodLimits computes the pod limits per the PodResourcesOptions supplied. If PodResourcesOptions is nil, then
// the limits are returned including pod overhead for any non-zero limits. The computation is part of the API and must be reviewed
// as an API change.
func PodLimits(pod *v1.Pod, opts PodResourcesOptions) v1.ResourceList {
	// attempt to reuse the maps if passed, or allocate otherwise
	limits := AggregateContainerLimits(pod, opts)
	if !opts.SkipPodLevelResources && IsPodLevelResourcesSet(pod) {
		for resourceName, quantity := range pod.Spec.Resources.Limits {
			if IsSupportedPodLevelResource(resourceName) {
				limits[resourceName] = quantity
			}
		}
	}

	// Add overhead to non-zero limits if requested:
	if !opts.ExcludeOverhead && pod.Spec.Overhead != nil {
		for name, quantity := range pod.Spec.Overhead {
			if value, ok := limits[name]; ok && !value.IsZero() {
				value.Add(quantity)
				limits[name] = value
			}
		}
	}

	return limits
}

// AggregateContainerLimits computes the aggregated resource limits of all the containers
// in a pod. This computation follows the formula defined in the KEP for sidecar
// containers. See https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers#resources-calculation-for-scheduling-and-pod-admission
// for more details.
func AggregateContainerLimits(pod *v1.Pod, opts PodResourcesOptions) v1.ResourceList {
	// attempt to reuse the maps if passed, or allocate otherwise
	limits := reuseOrClearResourceList(opts.Reuse)
	var containerStatuses map[string]*v1.ContainerStatus
	if opts.UseStatusResources {
		containerStatuses = make(map[string]*v1.ContainerStatus, len(pod.Status.ContainerStatuses))
		for i := range pod.Status.ContainerStatuses {
			containerStatuses[pod.Status.ContainerStatuses[i].Name] = &pod.Status.ContainerStatuses[i]
		}
	}

	for _, container := range pod.Spec.Containers {
		containerLimits := container.Resources.Limits
		if opts.UseStatusResources {
			cs, found := containerStatuses[container.Name]
			if found && cs.Resources != nil {
				if pod.Status.Resize == v1.PodResizeStatusInfeasible {
					containerLimits = cs.Resources.Limits.DeepCopy()
				} else {
					containerLimits = max(container.Resources.Limits, cs.Resources.Limits)
				}
			}
		}

		if opts.ContainerFn != nil {
			opts.ContainerFn(containerLimits, Containers)
		}
		addResourceList(limits, containerLimits)
	}

	restartableInitContainerLimits := v1.ResourceList{}
	initContainerLimits := v1.ResourceList{}
	// init containers define the minimum of any resource
	//
	// Let's say `InitContainerUse(i)` is the resource requirements when the i-th
	// init container is initializing, then
	// `InitContainerUse(i) = sum(Resources of restartable init containers with index < i) + Resources of i-th init container`.
	//
	// See https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers#exposing-pod-resource-requirements for the detail.
	for _, container := range pod.Spec.InitContainers {
		containerLimits := container.Resources.Limits
		// Is the init container marked as a restartable init container?
		if container.RestartPolicy != nil && *container.RestartPolicy == v1.ContainerRestartPolicyAlways {
			addResourceList(limits, containerLimits)

			// track our cumulative restartable init container resources
			addResourceList(restartableInitContainerLimits, containerLimits)
			containerLimits = restartableInitContainerLimits
		} else {
			tmp := v1.ResourceList{}
			addResourceList(tmp, containerLimits)
			addResourceList(tmp, restartableInitContainerLimits)
			containerLimits = tmp
		}

		if opts.ContainerFn != nil {
			opts.ContainerFn(containerLimits, InitContainers)
		}
		maxResourceList(initContainerLimits, containerLimits)
	}

	maxResourceList(limits, initContainerLimits)
	return limits
}

// addResourceList adds the resources in newList to list.
func addResourceList(list, newList v1.ResourceList) {
	for name, quantity := range newList {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
		} else {
			value.Add(quantity)
			list[name] = value
		}
	}
}

// maxResourceList sets list to the greater of list/newList for every resource in newList
func maxResourceList(list, newList v1.ResourceList) {
	for name, quantity := range newList {
		if value, ok := list[name]; !ok || quantity.Cmp(value) > 0 {
			list[name] = quantity.DeepCopy()
		}
	}
}

// max returns the result of max(a, b) for each named resource and is only used if we can't
// accumulate into an existing resource list
func max(a v1.ResourceList, b v1.ResourceList) v1.ResourceList {
	result := v1.ResourceList{}
	for key, value := range a {
		if other, found := b[key]; found {
			if value.Cmp(other) <= 0 {
				result[key] = other.DeepCopy()
				continue
			}
		}
		result[key] = value.DeepCopy()
	}
	for key, value := range b {
		if _, found := result[key]; !found {
			result[key] = value.DeepCopy()
		}
	}
	return result
}

// reuseOrClearResourceList is a helper for avoiding excessive allocations of
// resource lists within the inner loop of resource calculations.
func reuseOrClearResourceList(reuse v1.ResourceList) v1.ResourceList {
	if reuse == nil {
		return make(v1.ResourceList, 4)
	}
	for k := range reuse {
		delete(reuse, k)
	}
	return reuse
}
