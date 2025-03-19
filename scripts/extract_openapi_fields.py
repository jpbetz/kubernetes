import json
import os
import csv
import glob

# Define the input and output file paths
input_api_groups_file = 'data/api_groups_and_kinds.csv'
output_file = 'data/api_fields.csv'

# Directory containing the OpenAPI files
openapi_dir = 'api/openapi-spec/v3'

def get_fields_from_schema(schema, path="", fields=None, api_group="", kind=""):
    """Extract fields from a JSON schema recursively"""
    if fields is None:
        fields = []
    
    # Process properties at this level
    properties = schema.get('properties', {})
    for prop_name, prop_data in properties.items():
        # Skip apiVersion and kind which are common to all resources
        if prop_name in ['apiVersion', 'kind'] and path == "":
            continue
        
        field_path = f"{path}.{prop_name}" if path else prop_name
        
        # Handle reference types
        field_type = ""
        json_tag = prop_name
        optional = "true"  # Most fields are optional unless required is specified
        description = prop_data.get('description', '')
        
        # Check if the field is required
        if 'required' in schema and prop_name in schema['required']:
            optional = "false"
        
        # Get field type
        if '$ref' in prop_data:
            ref = prop_data['$ref']
            field_type = ref.split('/')[-1]  # Get the last part of the reference
        elif 'allOf' in prop_data and prop_data['allOf'] and '$ref' in prop_data['allOf'][0]:
            ref = prop_data['allOf'][0]['$ref']
            field_type = ref.split('/')[-1]  # Get the last part of the reference
        elif 'type' in prop_data:
            field_type = prop_data['type']
            if field_type == 'array' and 'items' in prop_data:
                items = prop_data['items']
                item_type = ""
                if '$ref' in items:
                    item_type = items['$ref'].split('/')[-1]
                elif 'allOf' in items and items['allOf'] and '$ref' in items['allOf'][0]:
                    item_type = items['allOf'][0]['$ref'].split('/')[-1]
                elif 'type' in items:
                    item_type = items['type']
                
                field_type = f"array of {item_type}"
        
        # Add to fields list
        fields.append({
            'APIGroup': api_group,
            'Kind': kind,
            'Field': field_path,
            'Type': field_type,
            'JSONTag': json_tag,
            'Optional': optional,
            'Description': description
        })
        
        # Process nested objects recursively if they have properties
        if 'properties' in prop_data:
            get_fields_from_schema(prop_data, field_path, fields, api_group, kind)
        elif 'allOf' in prop_data:
            for sub_schema in prop_data['allOf']:
                if 'properties' in sub_schema:
                    get_fields_from_schema(sub_schema, field_path, fields, api_group, kind)
        elif 'items' in prop_data and 'properties' in prop_data['items']:
            get_fields_from_schema(prop_data['items'], field_path + '[]', fields, api_group, kind)
    
    return fields

def get_resource_schema(api_group, kind, version="v1"):
    """Find the schema for a given API group and kind in the OpenAPI files"""
    # Normalize API group to match file naming
    if api_group == "":
        # Core API group
        schema_id = f"io.k8s.api.core.{version}.{kind}"
        filename = f"api__{version}_openapi.json"
    else:
        # Convert API group dots to underscores for file name
        api_group_path = api_group.replace('.', '_')
        schema_id = f"io.k8s.api.{api_group_path}.{version}.{kind}"
        filename = f"apis__{api_group_path}__{version}_openapi.json"
    
    # Find matching OpenAPI files
    matching_files = glob.glob(os.path.join(openapi_dir, filename))
    if not matching_files:
        # Try other versions
        matching_files = glob.glob(os.path.join(openapi_dir, f"apis__{api_group.replace('.', '_')}__*_openapi.json"))
    
    if not matching_files:
        print(f"  Could not find OpenAPI file for {api_group}/{kind}")
        return None
    
    # Look for the schema in each matching file
    for file_path in matching_files:
        try:
            with open(file_path, 'r') as f:
                openapi_data = json.load(f)
            
            # Check if the schema exists in this file
            schema = openapi_data.get('components', {}).get('schemas', {}).get(schema_id)
            if schema:
                print(f"  Found schema in {file_path}")
                return schema
        except Exception as e:
            print(f"  Error reading {file_path}: {e}")
    
    # If we get here, we couldn't find the schema
    print(f"  Could not find schema for {schema_id} in OpenAPI files")
    return None

def main():
    # Ensure output directory exists
    os.makedirs(os.path.dirname(output_file), exist_ok=True)
    
    # Read API groups and kinds from input CSV
    api_groups_kinds = []
    with open(input_api_groups_file, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            api_groups_kinds.append(row)
    
    # Process each API group and kind
    all_fields = []
    for item in api_groups_kinds:
        api_group = item['APIGroup']
        kind = item['Kind']
        version = item.get('Version', 'v1')
        
        print(f"Processing {api_group}/{kind}")
        
        # Get schema from OpenAPI files
        schema = get_resource_schema(api_group, kind, version)
        if schema:
            # Extract fields from schema
            fields = get_fields_from_schema(schema, "", [], api_group, kind)
            all_fields.extend(fields)
    
    # Write fields to output CSV
    with open(output_file, 'w', newline='') as f:
        writer = csv.writer(f)
        writer.writerow(['APIGroup', 'Kind', 'Field', 'Type', 'JSONTag', 'Optional', 'Description'])
        
        for field in all_fields:
            writer.writerow([
                field['APIGroup'],
                field['Kind'],
                field['Field'],
                field['Type'],
                field['JSONTag'],
                field['Optional'],
                field['Description']
            ])

if __name__ == "__main__":
    main() 