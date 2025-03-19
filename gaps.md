# Kubernetes API Validation Gaps

This document lists validation rules that are tested in code but not documented in validation.md.

## Methodology

For each API group and Kind mentioned in validation.md:
1. Examined the corresponding validation_test.go files in pkg/apis
2. Identified tests that validate fields or rules not mentioned in validation.md tables
3. Listed the missing validations below, organized by API group and Kind

## Core API Group

### PersistentVolume
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| metadata.namespace | Must be empty (PVs are cluster-scoped) | format | No |
| spec.volumeMode | Must be a valid PersistentVolumeMode ("Filesystem" or "Block") | enum | No |
| spec.accessModes | ReadWriteOncePod cannot be specified with other access modes | cross field | No |
| spec.nodeAffinity | Node affinity rules must be valid if specified | cross field | No |
| spec.volumeAttributesClassName | Must be a valid name if specified | format | Yes, only when VolumeAttributesClass feature gate is enabled |

### PersistentVolumeClaim
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.volumeMode | Must be a valid PersistentVolumeMode ("Filesystem" or "Block") | enum | No |
| spec.dataSourceRef | Must specify valid group/kind/name/namespace if specified | format | Yes, only when VolumeDataSourceRef feature gate is enabled |
| spec.dataSource | Must specify valid kind/name if specified | format | No |

### Pod
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.dnsConfig | Must have valid nameservers (max 3), searches (max 6/32 depending on policy), and options | format, cross field | No |
| spec.dnsConfig.searches | Total character length limited to 256/2048 depending on policy | cross field (size limit) | No |
| spec.readinessGates | Must have valid condition types in the format of domain/name | format | No |
| spec.schedulingGates | Cannot set nodeName when schedulingGates are present | cross field (conditional requirement) | No |
| spec.conditions | Custom conditions must use domain-prefixed names | format | No |
| spec.os | Must be a valid OS ("linux" or "windows") | enum | No |
| spec.securityContext.supplementalGroups | Must be valid integer UIDs, follows supplementalGroupsPolicy if specified | format | Yes, when SupplementalGroupsPolicy feature is enabled |
| spec.hostUsers | Boolean field controlling user namespace usage | format | Yes, when UserNamespacesSupport feature is enabled |
| spec.ephemeralContainers | Must follow container validation rules, names must be unique across all containers | format, cross field (uniqueness) | No |
| spec.overhead | Must contain valid resource quantities | format | No |

## Batch API Group

### Job
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.completionMode | Must be a valid CompletionMode ("NonIndexed" or "Indexed") | enum | No |
| spec.suspend | Boolean field controlling job suspension | format | No |
| spec.podFailurePolicy | Must have valid rules with valid actions and conditions | cross field | No |
| status.uncountedTerminatedPods | Must have valid pod UIDs, no duplicates between succeeded and failed | cross field (uniqueness) | No |

### CronJob
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.timeZone | Must be a valid time zone name if specified | format | Yes, when CronJobTimeZone feature is enabled |

## Networking API Group

### NetworkPolicy
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.egress[*].to[*].ports | Must have valid port numbers or names | format | No |
| spec.ingress[*].from[*].ports | Must have valid port numbers or names | format | No |

### Ingress
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.ingressClassName | Must be a valid name if specified | format | No |
| spec.rules[*].http.paths[*].pathType | Must be "Exact", "Prefix", or "ImplementationSpecific" | enum | No |

## Apps API Group

### StatefulSet
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.persistentVolumeClaimRetentionPolicy | Must have valid deletion and reclaim policies | enum | Yes, when StatefulSetAutoDeletePVC feature is enabled |
| spec.ordinals | Must have valid start and count values | format | Yes, when StatefulSetStartOrdinal feature is enabled |

### Deployment
| Field | Validation Rule | Validation Type | Conditional Validation |
|-------|-----------------|----------------|------------------------|
| spec.minReadySeconds | Must be non-negative integer | format | No | 