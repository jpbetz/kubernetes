/*
Copyright The Kubernetes Authors.

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

package apidefinitions

import (
	"context"
	"encoding/json"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// TestGenerationManagement tests that metadata.generation is managed when a resource is created and updated.
//
// The test ensures:
// 1. Generation initializes to 1 on create.
// 2. Generation does not increase for status updates.
// 3. Generation does not increase for metadata-only updates on the main endpoint.
// 4. Generation monotonically increases for spec/body updates on the main endpoint.
func TestGenerationManagement(t *testing.T) {
	// DO NOT ADD NEW ENTRIES HERE.
	// This tracks pre-existing resources that do not manage metadata.generation on create or update.
	generationExempt := sets.New(
		// Pre-existing resources with /status that do not manage generation:
		"apiservices.v1.apiregistration.k8s.io",
		"certificatesigningrequests.v1.certificates.k8s.io",
		"csinodes.v1.storage.k8s.io",
		"namespaces.v1",
		"nodes.v1",
		"persistentvolumeclaims.v1",
		"persistentvolumes.v1",
		"resourceclaims.v1.resource.k8s.io",
		"resourceclaims.v1beta1.resource.k8s.io",
		"resourceclaims.v1beta2.resource.k8s.io",
		"resourcequotas.v1",
		"servicecidrs.v1.networking.k8s.io",
		"services.v1",
		"storageversions.v1alpha1.internal.apiserver.k8s.io",
		"volumeattachments.v1.storage.k8s.io",

		// Pre-existing resources without /status that do not manage generation:
		"clusterrolebindings.v1.rbac.authorization.k8s.io",
		"clusterroles.v1.rbac.authorization.k8s.io",
		"configmaps.v1",
		"controllerrevisions.v1.apps",
		"csistoragecapacities.v1.storage.k8s.io",
		"endpoints.v1",
		"events.v1",
		"events.v1.events.k8s.io",
		"ipaddresses.v1.networking.k8s.io",
		"leasecandidates.v1alpha2.coordination.k8s.io",
		"leasecandidates.v1beta1.coordination.k8s.io",
		"leases.v1.coordination.k8s.io",
		"limitranges.v1",
		"resourceclaimtemplates.v1.resource.k8s.io",
		"resourceclaimtemplates.v1beta2.resource.k8s.io",
		"rolebindings.v1.rbac.authorization.k8s.io",
		"roles.v1.rbac.authorization.k8s.io",
		"runtimeclasses.v1.node.k8s.io",
		"secrets.v1",
		"serviceaccounts.v1",
		"storageclasses.v1.storage.k8s.io",
		"volumeattributesclasses.v1.storage.k8s.io",
	)

	// DO NOT ADD NEW ENTRIES HERE.
	// Resources that do not initialize generation to 1 on create, but do increment generation on update.
	createGenerationExempt := sets.New(
		"csidrivers.v1.storage.k8s.io",
	)

	// DO NOT ADD NEW ENTRIES HERE.
	// Resources that initialize generation to 1 on create, but do not increment generation on update.
	updateGenerationExempt := sets.New(
		"priorityclasses.v1.scheduling.k8s.io",
	)

	// DO NOT ADD NEW ENTRIES HERE.
	// Resources that increment generation when metadata (labels or annotations) is updated on the main endpoint.
	metadataIncrementsGenerationExempt := sets.New(
		"deployments.v1.apps",
		"endpointslices.v1.discovery.k8s.io",
	)

	// Resources whose spec/body fields are immutable after creation.
	immutableSpec := sets.New(
		"certificatesigningrequests.v1.certificates.k8s.io",
		"evictions.v1alpha1.lifecycle.k8s.io",
		"ipaddresses.v1.networking.k8s.io",
		"persistentvolumeclaims.v1",
		"podcertificaterequests.v1.certificates.k8s.io",
		"podcertificaterequests.v1beta1.certificates.k8s.io",
		"resourceclaims.v1.resource.k8s.io",
		"resourceclaims.v1beta1.resource.k8s.io",
		"resourceclaims.v1beta2.resource.k8s.io",
		"resourceclaimtemplates.v1.resource.k8s.io",
		"resourceclaimtemplates.v1beta2.resource.k8s.io",
		"resourcepoolstatusrequests.v1alpha3.resource.k8s.io",
		"runtimeclasses.v1.node.k8s.io",
		"servicecidrs.v1.networking.k8s.io",
		"storageversionmigrations.v1.storagemigration.k8s.io",
		"storageversionmigrations.v1beta1.storagemigration.k8s.io",
		"storageversions.v1alpha1.internal.apiserver.k8s.io",
		"volumeattachments.v1.storage.k8s.io",
		"volumeattributesclasses.v1.storage.k8s.io",
	)

	TestAllDefinitions(t, "generation-namespace", func(t *testing.T, api Definition) {
		if !api.HasVerb("create") || !api.HasVerb("patch") || !api.HasVerb("get") || !api.HasVerb("update") {
			t.Skip("Resource does not support create, patch, get, and update")
		}

		status := "{}"
		if api.HasStatus() {
			status = api.StorageData.GetStatusStub()
		}
		obj := TestObj(t, api.StorageData.Stub, status, api.Mapping.GroupVersionKind)
		name := obj.GetName()
		rsc := api.ResourceClient()

		_, err := rsc.Create(context.TODO(), obj, metav1.CreateOptions{FieldManager: "spec-manager"})
		if err != nil {
			t.Fatalf("Failed to create: %v", err)
		}
		defer func() {
			if err := rsc.Delete(context.TODO(), name, *metav1.NewDeleteOptions(0)); err != nil {
				t.Logf("Failed to delete %v: %v", name, err)
			}
		}()

		baseline, err := rsc.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get baseline: %v", err)
		}

		// 1. Verify that generation initializes to 1 on create.
		createExempt := generationExempt.Union(createGenerationExempt)
		assertDefault(t, api.Mapping.Resource, "metadata.generation must initialize to 1 on create", baseline.GetGeneration() == 1, createExempt)
		if matchesException(api.Mapping.Resource, createExempt) && baseline.GetGeneration() != 0 {
			t.Errorf("Expected create-exempt resource to have generation 0 on create, got %v", baseline.GetGeneration())
		}

		currentGen := baseline.GetGeneration()

		// 2. Verify that updating status does NOT bump generation.
		if api.HasStatus() {
			update := api.StorageData.GetMutatedStatusStub()
			statusObj := TestObj(t, api.StorageData.Stub, update, api.Mapping.GroupVersionKind)
			statusObj.SetName(name)

			_, err = rsc.ApplyStatus(context.TODO(), name, statusObj, metav1.ApplyOptions{FieldManager: "status-manager", Force: true})
			if err != nil {
				t.Fatalf("Failed to apply status via SSA: %v", err)
			}

			afterStatus, err := rsc.Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get after status update: %v", err)
			}

			if afterStatus.GetGeneration() != currentGen {
				t.Errorf("Expected generation to remain %v after status update, but got %v", currentGen, afterStatus.GetGeneration())
			}
			currentGen = afterStatus.GetGeneration()
		}

		// 3. Verify that updating only metadata on the main endpoint does NOT bump generation.
		metadataPatch := []byte(`{"metadata": {"labels": {"test-gen-label": "true"}, "annotations": {"test-gen-annotation": "true"}}}`)
		afterMetadata, err := rsc.Patch(context.TODO(), name, types.MergePatchType, metadataPatch, metav1.PatchOptions{})
		if err != nil {
			t.Fatalf("Patch to main endpoint with metadata failed: %v", err)
		}
		assertDefault(t, api.Mapping.Resource, "metadata-only updates must not increment metadata.generation", afterMetadata.GetGeneration() == currentGen, metadataIncrementsGenerationExempt)
		currentGen = afterMetadata.GetGeneration()

		// 4. Verify that updating spec/body on the main endpoint bumps generation.
		differentSpec := api.StorageData.MutatedStub
		isImmutable := matchesException(api.Mapping.Resource, immutableSpec)
		if isImmutable {
			if differentSpec != "" && differentSpec != "{}" {
				// Some immutable-spec resources define MutatedStub for pre-validation SSA conflict testing.
				// Verify that patching the main endpoint with MutatedStub is rejected as invalid.
				if _, err := rsc.Patch(context.TODO(), name, types.MergePatchType, []byte(differentSpec), metav1.PatchOptions{}); !apierrors.IsInvalid(err) {
					t.Fatalf("%s is listed in immutableSpec, but patching with MutatedStub was not rejected as invalid (err=%v); remove it from immutableSpec if mutable", ResourceString(api.Mapping.Resource), err)
				}
			}
			return
		}
		if differentSpec == "" || differentSpec == "{}" {
			t.Fatalf("No MutatedStub defined for mutable resource %s (define MutatedStub in test/integration/etcd/data.go, or add to immutableSpec if immutable)", ResourceString(api.Mapping.Resource))
		}
		if !mutatesNonMetadata(t, differentSpec) {
			t.Fatalf("MutatedStub for %s only mutates metadata; it must mutate a spec or body field", ResourceString(api.Mapping.Resource))
		}

		result, err := rsc.Patch(context.TODO(), name, types.MergePatchType, []byte(differentSpec), metav1.PatchOptions{})
		if err != nil {
			t.Fatalf("Patch to main endpoint with MutatedStub failed: %v", err)
		}

		updateExempt := generationExempt.Union(updateGenerationExempt)
		assertDefault(t, api.Mapping.Resource, "spec updates must increment metadata.generation", result.GetGeneration() > currentGen, updateExempt)
		if matchesException(api.Mapping.Resource, generationExempt) && result.GetGeneration() != 0 {
			t.Errorf("Expected generation-exempt resource to have generation 0 after update, got %v", result.GetGeneration())
		}
	})
}

func mutatesNonMetadata(t *testing.T, patchJSON string) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(patchJSON), &m); err != nil {
		t.Fatalf("Failed to unmarshal MutatedStub: %v", err)
	}
	for k := range m {
		if k != "metadata" && k != "status" && k != "apiVersion" && k != "kind" {
			return true
		}
	}
	return false
}
