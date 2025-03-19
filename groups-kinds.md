# Kubernetes API Groups and Kinds

This document provides a comprehensive list of Kubernetes API groups and their associated resource kinds. The information is organized by API group, showing each group's available resource kinds and their respective versions.

## Core (Legacy) Group
The Core Group has no group name (empty string "").

| Kind | Versions | Description |
|------|----------|-------------|
| Binding | v1 | Represents the binding of a pod to a node |
| ComponentStatus | v1 | Component health status |
| ConfigMap | v1 | Store of small chunks of non-confidential data |
| Endpoints | v1 | Network endpoints that implement a service |
| Event | v1 | Record of events in the system |
| LimitRange | v1 | Constraints on resource usage |
| Namespace | v1 | Isolation boundary for resources |
| Node | v1 | Worker machine in the cluster |
| PersistentVolume | v1 | Storage resource in the cluster |
| PersistentVolumeClaim | v1 | Request for storage |
| Pod | v1 | Smallest deployable unit in Kubernetes |
| PodTemplate | v1 | Template for pod creation |
| ReplicationController | v1 | Ensures a specified number of pod replicas are running |
| ResourceQuota | v1 | Constraints on resource consumption |
| Secret | v1 | Store of sensitive information |
| Service | v1 | Network abstraction for pods |
| ServiceAccount | v1 | Identity for processes running in a pod |

## apps
Applications API group

| Kind | Versions | Description |
|------|----------|-------------|
| ControllerRevision | v1 | Tracks revisions of resources for rollback |
| DaemonSet | v1, v1beta1, v1beta2 | Ensures all or some nodes run a copy of a pod |
| Deployment | v1, v1beta1, v1beta2 | Declarative updates for pods and replica sets |
| ReplicaSet | v1, v1beta1, v1beta2 | Ensures a specified number of pod replicas are running |
| StatefulSet | v1, v1beta1, v1beta2 | Manages stateful applications |

## autoscaling
Resources related to automatic scaling

| Kind | Versions | Description |
|------|----------|-------------|
| HorizontalPodAutoscaler | v1, v2, v2beta1, v2beta2 | Automatically scales pods |
| Scale | v1 | Represents a scaling request |

## batch
Batch processing workloads

| Kind | Versions | Description |
|------|----------|-------------|
| CronJob | v1, v1beta1 | Runs jobs on a schedule |
| Job | v1, v1beta1 | Runs a finite task to completion |

## networking.k8s.io
Network-related resources

| Kind | Versions | Description |
|------|----------|-------------|
| Ingress | v1, v1beta1 | Manages external access to services |
| IngressClass | v1 | Specifies ingress controller implementation |
| NetworkPolicy | v1 | Specification of how groups of pods communicate |

## rbac.authorization.k8s.io
Role-Based Access Control

| Kind | Versions | Description |
|------|----------|-------------|
| ClusterRole | v1, v1alpha1, v1beta1 | Cluster-wide role definition |
| ClusterRoleBinding | v1, v1alpha1, v1beta1 | Binds subjects to cluster roles |
| Role | v1, v1alpha1, v1beta1 | Namespace-scoped role definition |
| RoleBinding | v1, v1alpha1, v1beta1 | Binds subjects to roles |

## storage.k8s.io
Storage-related resources

| Kind | Versions | Description |
|------|----------|-------------|
| CSIDriver | v1, v1beta1 | Container Storage Interface driver information |
| CSINode | v1, v1beta1 | CSI node information |
| StorageClass | v1, v1beta1 | Storage class parameters |
| VolumeAttachment | v1, v1beta1 | Attachment of a volume to a node |
| VolumeAttributesClass | v1beta1 | Volume attributes for dynamic provisioning |

## apiextensions.k8s.io
API Extensions (Custom Resources)

| Kind | Versions | Description |
|------|----------|-------------|
| CustomResourceDefinition | v1, v1beta1 | Defines a custom resource type |

## admissionregistration.k8s.io
Admission Control

| Kind | Versions | Description |
|------|----------|-------------|
| MutatingWebhookConfiguration | v1, v1beta1 | Configuration for mutation webhooks |
| ValidatingWebhookConfiguration | v1, v1beta1 | Configuration for validation webhooks |
| ValidatingAdmissionPolicy | v1beta1, v1alpha1 | Policy for validating admission requests |
| ValidatingAdmissionPolicyBinding | v1beta1, v1alpha1 | Binding of validation policies |
| MutatingAdmissionPolicy | v1beta1, v1alpha1 | Policy for mutating admission requests |
| MutatingAdmissionPolicyBinding | v1beta1, v1alpha1 | Binding of mutation policies |

## authentication.k8s.io
Authentication resources

| Kind | Versions | Description |
|------|----------|-------------|
| TokenRequest | v1 | Request for a token |
| TokenReview | v1 | Authenticate API requests |
| SelfSubjectReview | v1 | Review of the requesting subject |

## authorization.k8s.io
Authorization resources

| Kind | Versions | Description |
|------|----------|-------------|
| LocalSubjectAccessReview | v1 | Namespace-specific access check |
| SelfSubjectAccessReview | v1 | Access check for the current user |
| SelfSubjectRulesReview | v1 | Rules that apply to the current user |
| SubjectAccessReview | v1 | General access check |

## certificates.k8s.io
Certificate management

| Kind | Versions | Description |
|------|----------|-------------|
| CertificateSigningRequest | v1 | Request for certificate signing |

## coordination.k8s.io
Coordination resources

| Kind | Versions | Description |
|------|----------|-------------|
| Lease | v1, v1beta1 | Distributed lock mechanism |
| LeaseCandidate | v1beta1 | Candidate for lease ownership |

## scheduling.k8s.io
Scheduling resources

| Kind | Versions | Description |
|------|----------|-------------|
| PriorityClass | v1, v1beta1, v1alpha1 | Pod scheduling priority |

## node.k8s.io
Node-related resources

| Kind | Versions | Description |
|------|----------|-------------|
| RuntimeClass | v1, v1beta1, v1alpha1 | Container runtime configuration |

## discovery.k8s.io
Service discovery

| Kind | Versions | Description |
|------|----------|-------------|
| EndpointSlice | v1, v1beta1 | Subset of endpoints for a service |

## events.k8s.io
Eventing system

| Kind | Versions | Description |
|------|----------|-------------|
| Event | v1, v1beta1 | Record of events in the system |

## flowcontrol.apiserver.k8s.io
API Priority and Fairness

| Kind | Versions | Description |
|------|----------|-------------|
| FlowSchema | v1beta1, v1beta2 | Flow control schema |
| PriorityLevelConfiguration | v1beta1, v1beta2, v1alpha1 | Priority level configuration |

## apiregistration.k8s.io
API service registration

| Kind | Versions | Description |
|------|----------|-------------|
| APIService | v1, v1beta1 | API service registration |

## resource.k8s.io
Resources for dynamic resource allocation

| Kind | Versions | Description |
|------|----------|-------------|
| ResourceClaim | v1beta1, v1alpha2, v1alpha1 | Request for dynamic resources |
| ResourceClaimTemplate | v1beta1, v1alpha2, v1alpha1 | Template for resource claims |
| ResourceClass | v1beta1, v1alpha2, v1alpha1 | Class of dynamic resources |
| ResourceClaimParameters | v1beta1, v1alpha2, v1alpha1 | Parameters for resource claims |

## policy
Policy resources

| Kind | Versions | Description |
|------|----------|-------------|
| PodDisruptionBudget | v1, v1beta1 | Disruption constraints for pods |
| PodSecurityPolicy | v1beta1 | Pod security policies |

## metrics.k8s.io
Metrics resources

| Kind | Versions | Description |
|------|----------|-------------|
| NodeMetrics | v1beta1 | Node resource usage metrics |
| PodMetrics | v1beta1 | Pod resource usage metrics |

## migration.k8s.io
Migration resources 

| Kind | Versions | Description |
|------|----------|-------------|
| StorageVersionMigration | v1alpha1 | Storage version migration |
| StorageState | v1alpha1 | Storage state migration |

## extensions
Legacy API group (most resources moved to other groups)

| Kind | Versions | Description |
|------|----------|-------------|
| Ingress | v1beta1 | Legacy ingress (moved to networking.k8s.io) |
| NetworkPolicy | v1beta1 | Legacy network policy (moved to networking.k8s.io) |
| DaemonSet | v1beta1 | Legacy daemon set (moved to apps) |
| Deployment | v1beta1 | Legacy deployment (moved to apps) |
| ReplicaSet | v1beta1 | Legacy replica set (moved to apps) |
| PodSecurityPolicy | v1beta1 | Pod security policy (moved to policy) |

## Internal Resource Types
These are used internally by Kubernetes and not directly exposed through the API

| Kind | Description |
|------|-------------|
| DeleteOptions | Options for resource deletion |
| Status | General status response |
| WatchEvent | Event for watch operations |
| ListOptions | Options for list operations |
| ExportOptions | Options for export operations |
| GetOptions | Options for get operations |
| PatchOptions | Options for patch operations |

*Note: This document represents currently available API resources in Kubernetes and may not be exhaustive. Resources may vary between Kubernetes versions. Beta and alpha API groups/versions may change or be removed in future Kubernetes releases.* 