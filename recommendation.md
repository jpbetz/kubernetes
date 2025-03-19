# Adding cross validation support to validation-gen

## Overview

This document outlines recommendations for adding cross-field validation capabilities to the existing `validation-gen` framework in Kubernetes. By enhancing the current tag-based approach with more expressive validation constructs, we can enable complex validation scenarios while maintaining backward compatibility.

## Design Principles

1. **Declarative Validation**: Express complex validation rules through declarative tags
2. **Type-Safety**: Leverage Go's type system for validation definitions
3. **Composability**: Enable combining multiple validation rules
4. **Extensibility**: Allow adding new validation types without core changes
5. **Performance**: Optimize validation performance for common operations

## Proposed Cross-Validation Enhancements

### 1. Discriminated Union Pattern

Enable conditional field validation based on discriminator fields:

```go
// +k8s:unionType(discriminator=type)="ServiceType"  // Defines a union type based on a discriminator field
type ServiceSpec struct {
    Type string `json:"type"` // This field acts as the discriminator - its value determines which variant field should be set
    
    // This field should only be set when Type="ClusterIP"
    // +k8s:unionVariant="ClusterIP"
    ClusterIPConfig *ClusterIPConfig `json:"clusterIPConfig,omitempty"`
    
    // This field should only be set when Type="NodePort"
    // +k8s:unionVariant="NodePort"
    NodePortConfig *NodePortConfig `json:"nodePortConfig,omitempty"`
    
    // This field should only be set when Type="LoadBalancer"
    // +k8s:unionVariant="LoadBalancer"
    LoadBalancerConfig *LoadBalancerConfig `json:"loadBalancerConfig,omitempty"`
    
    // This field should only be set when Type="ExternalName"
    // +k8s:unionVariant="ExternalName"
    ExternalNameConfig *ExternalNameConfig `json:"externalNameConfig,omitempty"`
}
```

Benefits:
- Validates relationships between fields based on a discriminator value
- Makes conditional field requirements explicit
- Improves documentation and client generation

### 2. Named Constraints for Cross-Field Validation

Define reusable cross-field validation constraints:

```go
// The labelsMatch constraint ensures that labels in one field are a subset of labels in another field
// In this example:
// - childPath: refers to the path of the field containing the child labels (which should be a subset)
// - parentPath: refers to the path of the field containing the parent labels (which should be a superset)
// +k8s:labelsMatch(childPath=spec.template.metadata.labels, parentPath=spec.selector.matchLabels)
type Deployment struct {
    // ...
}

// A general rule tag using CEL expressions for maximum flexibility
// +k8s:rule="self.spec.minReplicas <= self.spec.maxReplicas && self.spec.maxReplicas <= 100"
type HorizontalPodAutoscaler struct {
    // ...
}
```

The validation system would include predefined constraints for common patterns:

**Field Relationship Constraints:**
- `labelsMatch`: Ensure labels in one field are a subset of another
- `relationship`: Define comparison relationships between fields (see section 3)

**Field Requirement Constraints:**
- `required`: Mark a field as required
- `requiredIf`: Field is required when a condition is met
- `conditionalRequirement`: Define conditions when a field is required
- `dependentRequired`: Field A is required when Field B is set

**Field Exclusivity Constraints:**
- `exclusiveFields`: Ensure only one of several fields is set
- `conditionalExclusive`: Define conditions for field exclusivity

**Field Format Constraints:**
- `format`: Validate field format (e.g., "ipv4", "qualified-name")
- `enum`: Limit field values to a predefined set
- `minimum`: Set minimum value for a numeric field
- `maximum`: Set maximum value for a numeric field

**Advanced Constraints:**
- `rule`: Express complex validation rules using CEL expressions
- `cel`: Define CEL expressions with specific context variables
- `conditionalValidation`: Apply validation only when a condition is met

> **Note:** For complex validations involving relationships between fields (like ensuring one field is less than another), the `rule` constraint with CEL expressions is preferred over specialized constraints. CEL expressions provide more flexibility and maintainability while reducing the proliferation of specialized constraint types.

### 3. Relationship Validations

Support numerical and logical relationships between fields:

```go
// The relationship constraint defines a comparison between two fields
// In this example:
// - field: the path to the field being validated (status.readyReplicas)
// - relation: the comparison operator (lte = less than or equal to)
// - compareTo: the field to compare against (spec.replicas)
// This constraint ensures that readyReplicas <= replicas
// +k8s:relationship(field=status.readyReplicas, relation=lte, compareTo=spec.replicas)
type Deployment struct {
    // ...
}
```

Supported relationship operators:
- `eq`, `neq`: Equal to/not equal to
- `lt`, `lte`, `gt`, `gte`: Numeric comparisons
- `subset`, `superset`: Set relationship operations
- `contains`, `notContains`: Containment operations

For simple validations, simplified syntax is available:
- `+k8s:minimum=x`: Equal to relation=gte,compareTo=x
- `+k8s:maximum=x`: Equal to relation=lte,compareTo=x 
- `+k8s:equals=x`: Equal to relation=eq,compareTo=x
- `+k8s:format=x`: Equal to relation=format,compareTo=x

The relationship constraint provides a flexible way to define validation rules that involve comparing:
- One field to another field
- One field to a constant value
- Set operations between fields containing collections

### 4. Enhanced Conditional Validation

Express complex field dependencies:

```go
// Place constraints directly on relevant structs
type Container struct {
    // +k8s:requiredIf(condition=resourceClaimsOnly=false)
    Image string `json:"image,omitempty"`
    ResourceClaimsOnly bool `json:"resourceClaimsOnly,omitempty"`
    // ...
}

// Apply constraints directly to each field
type PodSpec struct {
    // +k8s:conditionalForbidden(condition=hostUsers=false)
    HostIPC bool `json:"hostIPC,omitempty"`
    
    // +k8s:conditionalForbidden(condition=hostUsers=false)
    HostNetwork bool `json:"hostNetwork,omitempty"`
    
    // +k8s:conditionalForbidden(condition=hostUsers=false)
    HostPID bool `json:"hostPID,omitempty"`
    
    HostUsers bool `json:"hostUsers,omitempty"`
    // ...
}
```

Conditional validation provides a way to express that certain fields should be required, forbidden, or validated only when specific conditions are met:

- `requiredIf`: Field is required when the specified condition is true
- `conditionalForbidden`: Field cannot be set when the specified condition is true
- `conditionalValidation`: Apply validation only when the specified condition is true
- `conditionalExclusive`: Fields cannot have the same value when certain conditions are met

### 5. State Transition Validations

Define allowed state transitions:

```go
// Place the constraint directly on the ContainerState struct
type ContainerState struct {
    // Current state of the container 
    // +k8s:stateTransition(allowedTransitions=waiting->running,running->terminated)
    State string `json:"state,omitempty"`
    
    // Additional metadata for the current state
    Waiting     *ContainerStateWaiting    `json:"waiting,omitempty"`
    Running     *ContainerStateRunning    `json:"running,omitempty"`
    Terminated  *ContainerStateTerminated `json:"terminated,omitempty"`
    // ...
}
```

### 6. Immutability Rules

Add clear support for field immutability:

```go
// The immutable tag defines a field that cannot be changed once set
// +k8s:immutable="spec.selector"

// The immutableIf tag defines conditional immutability
// In this example, spec.selector becomes immutable during UPDATE operations
// +k8s:immutableIf(operation=UPDATE)="spec.selector"
type Deployment struct {
    // ...
}
```

## Integration with validation-gen

The existing `validation-gen` system would be extended to support these cross-validation approaches:

1. **Tag Parsing**: Enhance the parser to recognize cross-field validation tags
2. **Code Generation**: Generate validation code that implements cross-field logic
3. **Backward Compatibility**: Maintain support for existing validation tags

## Examples: Cross-Field Validations

### Example 1: Service Resource with Cross-Field Validation

```go
// Service represents a Kubernetes Service
type Service struct {
    // ...
    
    // +k8s:unionType(discriminator=type)="ServiceType"
    Spec ServiceSpec `json:"spec"`
    
    Status ServiceStatus `json:"status,omitempty"`
}

// ServiceStatus represents the current status of a service
type ServiceStatus struct {
    // This field is only required when the service type is LoadBalancer
    // +k8s:requiredIf(condition=spec.type=LoadBalancer)
    LoadBalancer LoadBalancerStatus `json:"loadBalancer,omitempty"`
}

// LoadBalancerStatus represents the status of a load balancer
type LoadBalancerStatus struct {
    Ingress []LoadBalancerIngress `json:"ingress,omitempty"`
}

// LoadBalancerIngress represents the ingress point of a load balancer
type LoadBalancerIngress struct {
    // IP must be a valid IPv4 address
    // +k8s:format="ipv4"
    IP string `json:"ip,omitempty"`
    // ...
}
```

### Example 2: Job Resource with Complex Status Validation

```go
// Job represents a Kubernetes Job resource
type Job struct {
    // ...
    Spec JobSpec `json:"spec"`
    Status JobStatus `json:"status,omitempty"`
}

// JobSpec represents the specification of a Job
type JobSpec struct {
    BackoffLimit *int32 `json:"backoffLimit,omitempty"`
    // ...
}

// JobStatus represents the current state of a Job
type JobStatus struct {
    // Place constraints directly on the fields they apply to
    // +k8s:minimum=0
    Succeeded int32 `json:"succeeded,omitempty"`
    
    // +k8s:relationship(relation=lte, compareTo=spec.backoffLimit)
    Failed int32 `json:"failed,omitempty"`
    
    Conditions []JobCondition `json:"conditions,omitempty"`
    
    // This field is only validated when completionMode="Indexed"
    // +k8s:conditionalValidation(condition=spec.completionMode=Indexed)
    // +k8s:format="resource-index-list"
    CompletedIndexes string `json:"completedIndexes,omitempty"`
}

// JobCondition represents a condition on a Job
type JobCondition struct {
    Type string `json:"type"`
    
    // Cannot be "True" for both Complete and Failed conditions
    // +k8s:conditionalExclusive(exclusiveWith=Failed, whenType=Complete, whenBothEqual=True)
    Status string `json:"status"`
    // ...
}
```

### Example 3: ValidatingAdmissionPolicy with CEL Expression Validation

```go
// ValidatingAdmissionPolicy defines a policy for validating admission requests
type ValidatingAdmissionPolicy struct {
    // ...
    Spec ValidatingAdmissionPolicySpec `json:"spec"`
}

// ValidatingAdmissionPolicySpec is the specification of a ValidatingAdmissionPolicy
type ValidatingAdmissionPolicySpec struct {
    // The expression represents a CEL expression that is evaluated by the admission controller
    // +k8s:cel(object,oldObject,request,namespace,authorizer)
    Expression string `json:"expression,omitempty"`
    
    // This field can only be set when the validationActions include 'Audit'
    // +k8s:requiredIf(condition=validationActions.contains('Audit'))
    AuditAnnotations []AuditAnnotation `json:"auditAnnotations,omitempty"`
    
    // Each validation action must be a valid action type
    // +k8s:enum=Deny,Warn,Audit
    ValidationActions []string `json:"validationActions,omitempty"`
    
    // Parameters for the admission policy
    ParamKind *ParamKind `json:"paramKind,omitempty"`
    
    // Match criteria for the admission policy
    // +k8s:required=true
    MatchConstraints *MatchResources `json:"matchConstraints"`
}

// AuditAnnotation describes how to produce an audit annotation for an admission policy
type AuditAnnotation struct {
    // Key is required and must be a qualified name
    // +k8s:required=true
    // +k8s:format="qualified-name"
    Key string `json:"key"`
    
    // The CEL expression used to generate the annotation value
    // +k8s:cel(object,oldObject,params,request,namespace,authorizer)
    // +k8s:required=true
    ValueExpression string `json:"valueExpression"`
}

// MatchResources declares what resources match the policy
type MatchResources struct {
    // Only one of excludeResourceRules or resourceRules can be specified
    // +k8s:exclusiveFields=excludeResourceRules,resourceRules
    ExcludeResourceRules []NamedRuleWithOperations `json:"excludeResourceRules,omitempty"`
    ResourceRules []NamedRuleWithOperations `json:"resourceRules,omitempty"`
    
    // Selectors related to resources
    NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
    ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty"`
}
```

## Parameter Syntax Guide

The constraint parameters use several syntax patterns:

1. **Simple path notation**: Uses dot notation to navigate nested fields (e.g., `spec.replicas`)

2. **Array notation**: Uses square brackets to refer to array elements:
   - `containers[0]` - refers to the first container
   - `containers[*]` - refers to all containers (wildcard)

3. **CEL query syntax**: Used for more complex selections:
   - `conditions.filter(c, c.type == 'Complete')` - selects array elements where the type field equals "Complete"
   - `conditions.exists(c, c.type == 'Complete')` - checks if any condition exists with type "Complete"

4. **Relationship notation**: Uses special tokens to represent relationships:
   - `->` arrow notation for transitions (e.g., `waiting->running`)
   - Comparison operators: `eq`, `neq`, `lt`, `lte`, `gt`, `gte`

## Related Systems

Several industry solutions provide declarative validation capabilities that could inform or complement the proposed enhancements to validation-gen:

### 1. CEL (Common Expression Language)

Kubernetes already uses CEL for ValidatingAdmissionPolicy. CEL provides a secure, portable expression language that evaluates to a boolean or string result.

- **Strengths**: Already integrated in Kubernetes ecosystem, designed for configuration validation, good performance, expressions are serializable
- **Relevance**: Could augment or replace the custom tag syntax, especially for complex cross-field validations
- **Example**: Instead of custom tags, field constraints could be written as CEL expressions:
  ```go
  // +k8s:validate:cel="self.status.readyReplicas <= self.spec.replicas"
  ```

### 2. CUE Language

CUE is a constraint-based configuration language that combines types and values with logic programming.

- **Strengths**: Purpose-built for configuration validation, strong type system, highly expressive constraints
- **Relevance**: Could provide inspiration for more powerful validation constructs or be used for validation external to the Go type system
- **Example**: CUE can express complex hierarchical constraints declaratively:
  ```cue
  #Service: {
    spec: {
      type: string
      if type == "LoadBalancer" {
        loadBalancerIP?: string // Optional when type is LoadBalancer
      }
    }
  }
  ```

### 3. JSON Schema / OpenAPI

The Kubernetes API already leverages OpenAPI for documentation, and newer JSON Schema versions support advanced validation features.

- **Strengths**: Industry standard, wide adoption, extensive tooling support
- **Relevance**: JSON Schema 2020-12 and OpenAPI 3.1 add robust [cross-field validation capabilities](https://json-schema.org/understanding-json-schema/reference/conditionals) that could be adapted to validation-gen
- **Example**: JSON Schema conditional validation:
  ```json
  {
    "type": "object",
    "properties": {
      "type": { "type": "string" },
      "loadBalancerIP": { "type": "string" }
    },
    "if": {
      "properties": { "type": { "const": "LoadBalancer" } }
    },
    "then": {
      "required": ["loadBalancerIP"]
    }
  }
  ```

### 4. OPA (Open Policy Agent) / Rego

OPA is used in Kubernetes ecosystem (e.g., Gatekeeper) to enforce policies through the Rego language.

- **Strengths**: Powerful policy expression, designed for Kubernetes integration
- **Relevance**: Could unify structural validation with policy-based admission control
- **Example**: Rego rule to validate relationships between fields:
  ```rego
  deny[msg] {
    input.kind == "Deployment"
    status := input.status
    spec := input.spec
    status.readyReplicas > spec.replicas
    msg := "readyReplicas cannot exceed replicas"
  }
  ```

Our proposed validation-gen enhancements draw inspiration from these systems while maintaining compatibility with Kubernetes' existing architecture. The tag-based approach provides a familiar interface for Kubernetes developers while incorporating powerful validation concepts from these related systems.

## Conclusion

Enhancing validation-gen with cross-field validation capabilities allows for more expressive validation rules while maintaining the declarative tag-based approach. These enhancements will enable Kubernetes API developers to define complex validation constraints that improve API correctness and user experience. 

## Validation Coverage Analysis

This section analyzes how the proposed validation enhancements compare to existing validations in the Kubernetes API.

### Coverage Summary

The proposed validation framework would handle approximately 230 out of 450+ validations currently implemented in Kubernetes APIs. The coverage breakdown includes:

1. **Discriminated Union Pattern** (~20 validations)
   - All "union" type validations (e.g., PersistentVolume's volume type exclusivity)
   - Service type-dependent validations (e.g., LoadBalancer-specific fields)

2. **Named Constraints & Cross-Field Validation** (~85 validations)
   - Labels/selector consistency (e.g., Deployment template must match selector)
   - Field relationship constraints
   - Required field dependencies

3. **Relationship Validations** (~40 validations)
   - All numeric comparisons (e.g., status fields not exceeding spec fields)
   - Range constraints (e.g., port numbers between 1-65535)

4. **Conditional Validation** (~50 validations)
   - All validations with explicit conditional requirements
   - Fields required/forbidden based on other fields (e.g., hostIPC and hostUsers)

5. **State Transitions** (~5 validations)
   - Pod container state transitions

6. **Immutability Rules** (~30 validations)
   - All "immutable" field validations during updates

The remaining validations are primarily covered by format, length, and size tags already supported in the framework:

- **Format Validations** (~200 validations)
  - DNS names, IP addresses, etc. using the `+k8s:format` tag
  - Domain-specific formats like cron expressions

- **Size/Length Constraints**
  - Data size limitations (e.g., ConfigMap data size)
  - Character length constraints

### Remaining Gaps: External Validation Requirements

The primary gap in the proposed framework is handling validations that require information external to the object being validated:

1. **Existence Validation**
   - Validating that referenced resources actually exist (e.g., PersistentVolumeClaim.spec.storageClassName)
   - Confirming that referenced CRDs exist when referenced (e.g., in ValidatingAdmissionPolicy.spec.paramKind)

2. **Cluster State Awareness**
   - Validations that depend on cluster configuration (e.g., feature gates)
   - Validations that need to check against current cluster capabilities

3. **Cross-Resource Validation**
   - Validating that resource references are compatible (e.g., volume types in PVCs must match PV capabilities)
   - Ensuring API group/version/kind references are valid according to the cluster's API server

4. **Privilege-Based Validation**
   - Validations that depend on the requesting user's permissions
   - Special validations for cluster-admin vs. regular user operations

For these cases, the framework may need to support hooks into external validation systems or provide a mechanism for validators to access external state during validation. This could potentially be addressed through:

- A validation context object passed to validators with access to cluster state
- Support for asynchronous validation that can query external systems
- Integration with admission controllers for more complex scenarios

These gaps primarily affect validations in areas involving resource references, security contexts, and specialized API behaviors that depend on cluster configuration or state. 