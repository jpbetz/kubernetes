import os
import re
import csv
import glob
from pathlib import Path

# Define the input and output file paths
input_csv = 'data/api_groups_and_kinds.csv'
output_csv = 'data/api_fields.csv'

# Regular expressions for parsing Go structs
struct_pattern = re.compile(r'type\s+(\w+)\s+struct\s*{([^}]+)}', re.DOTALL)
json_tag_pattern = re.compile(r'json:"([^"]*)"')
optional_tag_pattern = re.compile(r'\+optional')

# Directories to search for API types
api_dirs = [
    'pkg/apis',
    'staging/src/k8s.io/api'
]

def find_struct_file(api_group, kind):
    """Find the file that defines the given struct"""
    if api_group == "":
        # Core API group
        search_paths = [
            'pkg/apis/core/types.go',
            'staging/src/k8s.io/api/core/v1/types.go'
        ]
    else:
        # Map API group to directory structure
        api_group_parts = api_group.split('.')
        search_paths = []
        
        # Check in pkg/apis
        if api_group_parts[0] in os.listdir('pkg/apis'):
            path = os.path.join('pkg/apis', api_group_parts[0], 'types.go')
            if os.path.exists(path):
                search_paths.append(path)
        
        # Check in staging/src/k8s.io/api
        if api_group_parts[0] in os.listdir('staging/src/k8s.io/api'):
            versions = os.listdir(os.path.join('staging/src/k8s.io/api', api_group_parts[0]))
            for version in versions:
                if os.path.isdir(os.path.join('staging/src/k8s.io/api', api_group_parts[0], version)):
                    path = os.path.join('staging/src/k8s.io/api', api_group_parts[0], version, 'types.go')
                    if os.path.exists(path):
                        search_paths.append(path)
    
    # Search for the struct in all potential files
    for path in search_paths:
        if os.path.exists(path):
            with open(path, 'r') as f:
                content = f.read()
                for match in struct_pattern.finditer(content):
                    struct_name, _ = match.groups()
                    if struct_name == kind:
                        return path
    
    # If not found in the immediate files, search more broadly
    for api_dir in api_dirs:
        for file_path in glob.glob(f'{api_dir}/**/*.go', recursive=True):
            if 'test' in file_path or 'generated' in file_path:
                continue
            try:
                with open(file_path, 'r') as f:
                    content = f.read()
                    for match in struct_pattern.finditer(content):
                        struct_name, _ = match.groups()
                        if struct_name == kind:
                            return file_path
            except UnicodeDecodeError:
                # Skip binary files
                continue
    
    return None

def extract_struct_fields(file_path, struct_name):
    """Extract field information from the struct definition"""
    fields = []
    
    if not os.path.exists(file_path):
        return fields
    
    with open(file_path, 'r') as f:
        content = f.read()
        
        # Find the struct definition
        for match in struct_pattern.finditer(content):
            current_struct_name, struct_body = match.groups()
            
            if current_struct_name != struct_name:
                continue
            
            # Split the struct body into lines
            lines = struct_body.split('\n')
            
            # Track comment blocks that might contain +optional
            comment_block = []
            is_optional = False
            
            for line in lines:
                line = line.strip()
                
                # Skip empty lines
                if not line:
                    continue
                
                # Collect comment lines
                if line.startswith('//'):
                    comment_block.append(line)
                    if '+optional' in line:
                        is_optional = True
                    continue
                
                # If we reach here, it's a field definition
                # Parse it manually to avoid regex complexities
                
                # Check if it's an embedded field (type without field name)
                parts = line.split()
                if len(parts) == 1 or (len(parts) > 1 and parts[0] == parts[1]):
                    # This is an embedded type, skip it
                    comment_block = []
                    is_optional = False
                    continue
                
                # Extract field name and type
                field_name = parts[0]
                field_type = parts[1]
                
                # Extract struct tags if present
                tags = ""
                json_tag = ""
                
                # Look for backticks which contain struct tags
                if '`' in line:
                    tags_part = line.split('`')[1]
                    tags = tags_part
                    
                    # Extract JSON tag
                    json_match = json_tag_pattern.search(tags)
                    if json_match:
                        json_tag = json_match.group(1)
                
                # Determine if field is optional
                optional = "false"
                if is_optional or (tags and '+optional' in tags):
                    optional = "true"
                
                # Add field to the list
                fields.append({
                    'Field': field_name,
                    'Type': field_type,
                    'JSONTag': json_tag,
                    'Optional': optional,
                    'StructTags': tags
                })
                
                # Reset for next field
                comment_block = []
                is_optional = False
    
    return fields

def main():
    # Ensure output directory exists
    os.makedirs(os.path.dirname(output_csv), exist_ok=True)
    
    # Read API groups and kinds from input CSV
    api_groups_kinds = []
    with open(input_csv, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            api_groups_kinds.append(row)
    
    # Open output CSV file
    with open(output_csv, 'w', newline='') as f:
        writer = csv.writer(f)
        writer.writerow(['APIGroup', 'Kind', 'Field', 'Type', 'JSONTag', 'Optional', 'StructTags'])
        
        # Process each API group and kind
        for item in api_groups_kinds:
            api_group = item['APIGroup']
            kind = item['Kind']
            
            print(f"Processing {api_group}/{kind}...")
            
            # Find the file with the struct definition
            struct_file = find_struct_file(api_group, kind)
            
            if struct_file:
                print(f"  Found struct in {struct_file}")
                
                # Extract field information
                fields = extract_struct_fields(struct_file, kind)
                
                # Write field information to CSV
                for field in fields:
                    writer.writerow([
                        api_group,
                        kind,
                        field['Field'],
                        field['Type'],
                        field['JSONTag'],
                        field['Optional'],
                        field['StructTags']
                    ])
            else:
                print(f"  Could not find struct definition for {kind}")

if __name__ == "__main__":
    main() 