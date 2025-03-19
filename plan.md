# Kubernetes API Validation Rules Analysis Plan

This plan outlines steps to analyze validation rules in the Kubernetes API and create two comprehensive output files:
1. `validations.csv` - A complete CSV file with validation rules for all API groups and kinds
2. `validation-summary.md` - A markdown summary of validation rules

Please only write scripts if you really need to. It is often better to just find the information and update the output files directly.

## Steps

### 1. Find all API Groups and Kinds

**Prompt:**
You're working on Step 1 of the Kubernetes API validation rules analysis plan. Your task is to find all API Groups and Kinds in the Kubernetes codebase. 

- Search for API Groups in `pkg/apis` and the `staging/src/k8s.io/api` directories
- Search for all API kinds in these directories
- Analyze the code to determine the structure of API groups
- Create a file called `data/api_groups_and_kinds.csv` with columns: APIGroup,Kind,ImportPath
- The ImportPath should point to where the Go structs for this Kind are defined
- Make sure to include both internal and versioned APIs

Write the findings to the specified file to be used in subsequent steps.

### 2. Extract Field Information for Each Kind

**Prompt:**
You're working on Step 2 of the Kubernetes API validation rules analysis plan. Your task is to extract field information for each Kind identified in Step 1.

- Read `data/api_groups_and_kinds.csv` from Step 1
- For each Kind, examine its Go struct definition to extract all fields
- For each field, record:
  - Field name
  - Field type
  - JSON tag
  - Whether it's a pointer/optional
  - Any Go struct tags that might indicate validation rules
- Create a file called `data/api_fields.csv` with columns: APIGroup,Kind,Field,Type,JSONTag,Optional,StructTags
- Make sure to handle nested fields appropriately
- Pay special attention to fields like `metadata`, `spec`, and `status`

This information will be used in later steps to match fields with validation rules.

### 3. Locate and Extract Validation Rules from validation.go Files

**Prompt:**
You're working on Step 3 of the Kubernetes API validation rules analysis plan. Your task is to locate and extract validation rules from validation.go files.

- Look for all `validation.go` files under `pkg/apis` and `staging/src/k8s.io/api`
- For each file:
  - Identify which API Group and Kind(s) it validates
  - Extract validation functions and rules
  - Map the validation rules to specific fields identified in Step 2
  - Determine the validation type (format, cross field, enum, etc.)
  - Note any conditional validations
- Create a file called `data/validation_rules_from_go.csv` with columns: APIGroup,Kind,Field,ValidationRule,ValidationType,ValidationSubType,ConditionalValidation,SourceFile,SourceFunction
- Make sure to track the validation source file and function for reference

This file will contain validation rules found in the Go code.

### 4. Extract Validation Rules from OpenAPI Specifications

**Prompt:**
You're working on Step 4 of the Kubernetes API validation rules analysis plan. Your task is to extract validation rules from OpenAPI specifications.

- Examine both swagger.json and OpenAPI v3 data in the api/ directory
- For each API Group and Kind:
  - Extract validation constraints specified in the OpenAPI schemas
  - Map these constraints to fields identified in Step 2
  - Capture rules like required fields, patterns, min/max values, etc.
- Create a file called `data/validation_rules_from_openapi.csv` with columns: APIGroup,Kind,Field,ValidationRule,ValidationType,ValidationSubType,ConditionalValidation,SourceFile
- Compare with rules from Step 3 and note any rules found only in OpenAPI specs

This file will contain validation rules defined in OpenAPI specifications.

### 5. Analyze Validation Test Files

**Prompt:**
You're working on Step 5 of the Kubernetes API validation rules analysis plan. Your task is to analyze validation test files for additional insight.

- Find all `validation_test.go` files next to the validation.go files
- For each test file:
  - Identify test cases that verify validation rules
  - Extract information about what is being tested
  - Look for test cases that might reveal validation rules not explicitly stated in validation.go
- Create a file called `data/validation_rules_from_tests.csv` with columns: APIGroup,Kind,Field,ValidationRule,ValidationType,TestCase,TestFile
- Focus on extracting rules that weren't found in previous steps

This file will capture validation rules inferred from test cases.

### 6. Merge and Deduplicate Validation Rules

**Prompt:**
You're working on Step 6 of the Kubernetes API validation rules analysis plan. Your task is to merge and deduplicate validation rules from different sources.

- Read and combine data from:
  - `data/validation_rules_from_go.csv`
  - `data/validation_rules_from_openapi.csv`
  - `data/validation_rules_from_tests.csv`
- Deduplicate rules that appear in multiple sources
- Resolve any conflicts or inconsistencies
- Create a comprehensive file called `data/merged_validation_rules.csv` with all unique validation rules
- Use columns: APIGroup,Kind,Field,ValidationRule,ValidationType,ValidationSubType,ConditionalValidation,Sources
- The Sources column should list where each rule was found (Go, OpenAPI, Tests)

This file will be the foundation for the final output files.

### 7. Add Status Fields Validations

**Prompt:**
You're working on Step 7 of the Kubernetes API validation rules analysis plan. Your task is to specifically identify and add validation rules for status fields.

- Analyze validation code specific to status updates 
- Look for files like `pkg/registry/core/*/storage/status.go`
- Examine any strategy.ValidateUpdate functions that handle status updates
- Identify validation rules that specifically apply to status fields
- Update `data/merged_validation_rules.csv` with these additional rules
- Create a separate `data/status_validations.csv` file for status-specific validations with the same columns as the merged file

This step ensures status field validations are properly captured.

### 8. Generate Final validations.csv File

**Prompt:**
You're working on Step 8 of the Kubernetes API validation rules analysis plan. Your task is to generate the final validations.csv file.

- Read `data/merged_validation_rules.csv`
- Read `data/status_validations.csv`
- Combine and format the data according to the structure seen in `kubernetes_api_validations_updated.csv`
- Ensure all columns are properly populated:
  - API Group
  - Kind
  - Field
  - Validation Rule (clear English description)
  - Validation Type
  - Validation Sub-Type
  - Conditional Validation
- Sort the data logically (by API Group, then Kind, then Field)
- Write the final output to `validations.csv` in the root directory

This is one of the two final output files requested.

### 9. Generate Final validation-summary.md File

**Prompt:**
You're working on Step 9 of the Kubernetes API validation rules analysis plan. Your task is to generate the final validation-summary.md file.

- Read the `validations.csv` file created in Step 8
- Format the data into a structured markdown document organized by API Group
- For each API Group:
  - Create a section with a heading (e.g., "## Core API Group Validation Rules")
  - Create a table with columns: Kind, Field, Validation Rule, Validation Type, Conditional Validation
  - Organize the table by Kind
- Follow the format seen in the provided validation.md example
- Ensure the markdown is clean and well-formatted
- Write the output to `validation-summary.md` in the root directory

This is the second of the two final output files requested.

### 10. Verify Completeness and Consistency

**Prompt:**
You're working on Step 10 of the Kubernetes API validation rules analysis plan. Your task is to verify the completeness and consistency of the generated files.

- Compare your generated files against the provided examples:
  - Compare `validations.csv` with `kubernetes_api_validations_updated.csv`
  - Compare `validation-summary.md` with `validation.md`
- Verify that all API Groups, Kinds, and known validations are included
- Check that the formatting matches the expected output
- Identify any missing information or inconsistencies
- Make any necessary corrections to `validations.csv` and `validation-summary.md`
- Provide a brief report on the verification results

This final step ensures the quality and completeness of the output files. 