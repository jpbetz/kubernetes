# Kubernetes API Validation Rules - Migration Notes

## Resource Analysis for Validation Migration 

After analyzing the comprehensive validation rules from all Kubernetes resources, I've identified several resource kinds with diverse validation rules, particularly focusing on those with cross-field validations. These resources are prioritized based on both validation diversity and relative simplicity.

## Prioritized Resources for Validation Migration

### 1. Service Resource

The **Service** resource remains an excellent candidate with a good balance of diversity and simplicity:

1. **Cross-field validations**:
   - Port requirements depend on service type (at least one port required for ClusterIP and NodePort types)
   - Port names must be unique within service
   - ClusterIPs immutability rules on update (unless switching type)
   - ExternalName validation applies conditionally based on service type

2. **Format validations**:
   - Metadata name must be a valid DNS subdomain name
   - ClusterIP must be "None" or valid IP address
   - Ports must have valid port numbers (1-65535)
   - External IPs must be valid IP addresses
   - LoadBalancerIP must be a valid IP address

3. **Enum validations**:
   - Type must be "ClusterIP", "NodePort", "LoadBalancer", or "ExternalName"
   - Protocol must be "TCP", "UDP", or "SCTP"
   - ExternalTrafficPolicy must be "Local" or "Cluster"
   - InternalTrafficPolicy must be "Local" or "Cluster"
   - SessionAffinity must be "ClientIP" or "None"
   - IPFamilyPolicy must be "SingleStack", "PreferDualStack", or "RequireDualStack"

4. **Boolean validations**:
   - allocateLoadBalancerNodePorts, publishNotReadyAddresses must be valid boolean values

The Service resource offers a good mix of validation rule types while remaining relatively compact.

### 2. ConfigMap Resource

The **ConfigMap** resource is among the simplest with a focused set of validation rules:

1. **Cross-field validations**:
   - Data and BinaryData can't have overlapping keys

2. **Format validations**:
   - Keys must be valid DNS subdomain names
   - Combined data has size limits

3. **Boolean validations**:
   - immutable must be a valid boolean value

ConfigMap offers a good starting point for validation migration due to its simplicity while still containing cross-field validations.

### 3. ValidatingAdmissionPolicy Resource

The **ValidatingAdmissionPolicy** resource has interesting validation characteristics:

1. **Cross-field validations**:
   - Must specify resource rules in matchConstraints
   - Must specify at least one validation unless auditAnnotations is specified
   - paramKind must have valid API version and kind if specified

2. **Format validations**:
   - Metadata name must be a valid DNS subdomain name

3. **Enum validations**:
   - failurePolicy must be "Fail" or "Ignore"

This resource provides good diversity with moderate complexity and newer API patterns.

### 4. NetworkPolicy Resource

**NetworkPolicy** has rich validation characteristics with complex cross-field relationships:

1. **Cross-field validations**:
   - ipBlock.except must be a subset of ipBlock.cidr

2. **Conditional validations**:
   - ipBlock validations only apply when ipBlock is used
   - namespaceSelector validations only apply when namespaceSelector is used
   - podSelector validations only apply when podSelector is used

3. **Format validations**:
   - CIDR notation must be valid
   - Port must be between 1 and 65535 inclusive if numeric

4. **Enum validations**:
   - Protocol must be "TCP", "UDP", or "SCTP"
   - PolicyTypes must be "Ingress", "Egress", or both

NetworkPolicy has complex validation rules but offers rich patterns for validation migration.

### 5. PersistentVolumeClaim Resource

**PersistentVolumeClaim** has a moderate set of validation rules:

1. **Cross-field validations**:
   - Must specify storage request in resources
   - AccessModes constraints (ReadWriteOncePod cannot be specified with other modes)

2. **Format validations**:
   - Metadata name must be a valid DNS subdomain name
   - Must contain at least one valid access mode

3. **Enum validations**:
   - volumeMode must be "Filesystem" or "Block"

PVC offers good diversity with a focus on storage-specific validations.

### 6. Job Resource

**Job** resource demonstrates several validation types:

1. **Cross-field validations**:
   - Pod template must match selector
   - In status, Complete and Failed conditions cannot both be true

2. **Format validations**:
   - Fields like parallelism, completions, backoffLimit must be non-negative integers
   - activeDeadlineSeconds must be positive integer if specified
   - Parallelism must not exceed 100000 for indexed jobs

3. **Enum validations**:
   - Template's restartPolicy must be "Never" or "OnFailure"
   - completionMode must be "NonIndexed" or "Indexed"

4. **Immutability validations**:
   - Several fields are immutable on update (completionMode, podFailurePolicy, etc.)

Job offers good validation diversity but introduces more complexity with its status fields and immutability rules.

### 7. Pod Resource

**Pod** has the most comprehensive validation rules:

1. **Cross-field validations**:
   - Container names must be unique
   - initContainer names must not collide with regular container names
   - Volume names must be unique
   - If hostNetwork is true, containers requesting ports must match host ports
   - Various incompatibility rules (hostIPC, hostPID, hostNetwork with hostUsers=false)

2. **Format validations**:
   - Many fields must be valid DNS labels or subdomain names
   - Various integer fields must be non-negative
   - Security context validations

3. **Enum validations**:
   - restartPolicy must be "Always", "OnFailure", or "Never"
   - dnsPolicy must be one of several values
   - OS field must be "Linux" or "Windows"

Pod offers the most comprehensive validation set but has significantly more fields and complexity.

## Conclusion

Based on this analysis, I recommend **Service** as the primary candidate for validation migration, with **ConfigMap** as a simpler alternative. For more complex validation patterns, **ValidatingAdmissionPolicy** and **NetworkPolicy** provide rich examples of cross-field validations while remaining manageable in scope. 

The **Pod** resource should be considered last due to its complexity, despite having the richest validation rules. 