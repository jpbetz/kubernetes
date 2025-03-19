import os
import re
import csv
import glob

# Define API groups and Kinds extraction script for Kubernetes

# Output file
output_file = "data/api_groups_and_kinds.csv"

# Function to find all Go struct types that represent API kinds
def find_kinds_in_file(file_path):
    kinds = []
    try:
        with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
            content = f.read()
            
            # Find struct types that have TypeMeta as a field
            # This is a common pattern for Kubernetes API kinds
            struct_pattern = r'type\s+(\w+)\s+struct\s+{\s*metav1\.TypeMeta'
            structs = re.findall(struct_pattern, content)
            
            # Filter out List types as they're typically companion types
            non_list_structs = [s for s in structs if not s.endswith('List')]
            
            kinds.extend(non_list_structs)
    except Exception as e:
        print(f"Error reading {file_path}: {e}")
    
    return kinds

# Function to extract API group from register.go files
def find_api_group_from_register(register_file):
    try:
        with open(register_file, 'r', encoding='utf-8', errors='ignore') as f:
            content = f.read()
            
            # Look for GroupName constant
            group_match = re.search(r'const\s+GroupName\s*=\s*"([^"]*)"', content)
            if group_match:
                return group_match.group(1)
    except Exception as e:
        print(f"Error reading register file {register_file}: {e}")
    
    return None

# Function to extract API group from a file path
def get_api_group(file_path):
    # First, check if there's a register.go file in the same directory
    directory = os.path.dirname(file_path)
    register_file = os.path.join(directory, "register.go")
    
    if os.path.exists(register_file):
        group = find_api_group_from_register(register_file)
        if group is not None:
            return group
    
    # Fallback to path-based detection
    if 'pkg/apis/' in file_path:
        parts = file_path.split('pkg/apis/')
        if len(parts) > 1:
            group_path = parts[1].split('/')[0]
            # Handle core API group specially
            if group_path == 'core':
                return ""  # Core group has empty string as group name
            return group_path
    elif 'staging/src/k8s.io/api/' in file_path:
        parts = file_path.split('staging/src/k8s.io/api/')
        if len(parts) > 1:
            group_path = parts[1].split('/')[0]
            # Handle core API group specially
            if group_path == 'core':
                return ""  # Core group has empty string as group name
            return group_path
    
    return ""

# Function to get the import path for a kind
def get_import_path(file_path, api_group):
    if 'pkg/apis/' in file_path:
        parts = file_path.split('pkg/apis/')
        if len(parts) > 1:
            path_parts = parts[1].split('/')
            # Check if this is an internal or versioned API
            if len(path_parts) > 1 and path_parts[1].startswith('v'):
                return f"k8s.io/kubernetes/pkg/apis/{path_parts[0]}/{path_parts[1]}"
            else:
                return f"k8s.io/kubernetes/pkg/apis/{path_parts[0]}"
    elif 'staging/src/k8s.io/api/' in file_path:
        parts = file_path.split('staging/src/k8s.io/api/')
        if len(parts) > 1:
            path_parts = parts[1].split('/')
            if len(path_parts) > 1 and path_parts[1].startswith('v'):
                return f"k8s.io/api/{path_parts[0]}/{path_parts[1]}"
            else:
                return f"k8s.io/api/{path_parts[0]}"
    
    return ""

# Function to get the version from a file path
def get_version(file_path):
    # Extract version from the path
    if '/v' in file_path:
        parts = file_path.split('/')
        for part in parts:
            if part.startswith('v') and re.match(r'v\d+', part):
                if part.endswith('alpha') or part.endswith('beta'):
                    return part
                return part
    return "internal"  # Default to internal version if not found

# Function to check if a file exists in a directory
def check_file_exists(directory, filename):
    return os.path.exists(os.path.join(directory, filename))

# Main function to scan the codebase
def scan_codebase():
    results = []
    seen = set()  # To track unique API Group + Kind combinations
    
    # Patterns to find both internal and versioned APIs
    types_files_patterns = [
        "pkg/apis/*/types.go",
        "pkg/apis/*/v*/types.go",
        "staging/src/k8s.io/api/*/v*/types.go"
    ]
    
    # Find all types.go files
    types_files = []
    for pattern in types_files_patterns:
        types_files.extend(glob.glob(pattern))
    
    # Process each file
    for file_path in types_files:
        directory = os.path.dirname(file_path)
        api_group = get_api_group(file_path)
        version = get_version(file_path)
        import_path = get_import_path(file_path, api_group)
        
        # Find kinds in the file
        kinds = find_kinds_in_file(file_path)
        
        # Add to results
        for kind in kinds:
            key = f"{api_group}:{kind}"
            if key not in seen:
                seen.add(key)
                results.append({
                    'APIGroup': api_group,
                    'Kind': kind,
                    'Version': version,
                    'ImportPath': import_path
                })
    
    # Sort results by API group and kind
    results.sort(key=lambda x: (x['APIGroup'], x['Kind']))
    
    # Write results to CSV file
    with open(output_file, 'w', newline='') as csvfile:
        fieldnames = ['APIGroup', 'Kind', 'Version', 'ImportPath']
        writer = csv.DictWriter(csvfile, fieldnames=fieldnames)
        
        writer.writeheader()
        for row in results:
            writer.writerow(row)
    
    print(f"Extracted {len(results)} unique API kinds to {output_file}")

if __name__ == "__main__":
    scan_codebase() 