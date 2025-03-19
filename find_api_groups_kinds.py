import os
import re
import csv
from pathlib import Path

# Define the output file
output_file = "data/api_groups_and_kinds.csv"

# Directories to search
pkg_apis_dir = "pkg/apis"
staging_api_dir = "staging/src/k8s.io/api"

# Regex patterns
group_pattern = r'const GroupName = "(.*?)"'
kind_pattern = r'&(\w+){}'  # Pattern to find Kind registrations in addKnownTypes

# Results storage
api_entries = []

def find_api_groups_in_dir(base_dir):
    """Find all API groups in the given directory"""
    for group_dir in os.listdir(base_dir):
        group_path = os.path.join(base_dir, group_dir)
        if not os.path.isdir(group_path):
            continue

        # Skip certain directories that aren't API groups
        if group_dir in ['.github', 'fuzzer', 'helper', 'install', 'pods', 'validation', 'v1']:
            continue

        # Find the register.go file which contains the GroupName
        register_file = os.path.join(group_path, "register.go")
        if os.path.isfile(register_file):
            process_register_file(register_file, base_dir, group_dir)
        
        # Check for version subdirectories (v1, v1alpha1, etc)
        for version_dir in os.listdir(group_path):
            version_path = os.path.join(group_path, version_dir)
            if not os.path.isdir(version_path):
                continue
            
            if re.match(r'v\d+(?:alpha\d+|beta\d+)?', version_dir):
                register_file = os.path.join(version_path, "register.go")
                if os.path.isfile(register_file):
                    process_register_file(register_file, base_dir, group_dir, version_dir)

def process_register_file(register_file, base_dir, group_dir, version_dir=None):
    """Process a register.go file to extract API group and kind information"""
    try:
        with open(register_file, 'r', encoding='utf-8') as f:
            content = f.read()
            
            # Extract group name
            group_match = re.search(group_pattern, content)
            api_group = group_match.group(1) if group_match else group_dir
            if api_group == "":
                api_group = "core"
                
            # Extract kinds from addKnownTypes
            kinds = re.findall(kind_pattern, content)
            
            # Find the types.go file to confirm import path
            types_file = os.path.join(os.path.dirname(register_file), "types.go")
            if not os.path.isfile(types_file):
                # Look for types.go in parent directory if not found
                types_file = os.path.join(os.path.dirname(os.path.dirname(register_file)), "types.go")
            
            import_path = os.path.dirname(register_file)
            if base_dir == pkg_apis_dir:
                import_path = f"k8s.io/kubernetes/{import_path}"
            else:
                # For staging directory
                import_path = import_path.replace("staging/src/", "")
            
            # Add each kind with its information
            for kind in kinds:
                # Skip non-kind entries
                if kind in ["TypeMeta", "ListMeta", "ListOptions"]:
                    continue
                
                entry = {
                    "APIGroup": api_group,
                    "Kind": kind,
                    "ImportPath": import_path
                }
                api_entries.append(entry)
                
    except Exception as e:
        print(f"Error processing {register_file}: {e}")

# Main execution
print("Finding API Groups and Kinds...")
find_api_groups_in_dir(pkg_apis_dir)
find_api_groups_in_dir(staging_api_dir)

# Remove duplicates (same APIGroup and Kind)
unique_entries = []
seen = set()
for entry in api_entries:
    key = (entry["APIGroup"], entry["Kind"])
    if key not in seen:
        seen.add(key)
        unique_entries.append(entry)

# Sort by APIGroup and Kind
unique_entries.sort(key=lambda x: (x["APIGroup"], x["Kind"]))

# Write to CSV
print(f"Writing {len(unique_entries)} entries to {output_file}...")
with open(output_file, 'w', newline='', encoding='utf-8') as f:
    writer = csv.DictWriter(f, fieldnames=["APIGroup", "Kind", "ImportPath"])
    writer.writeheader()
    writer.writerows(unique_entries)

print(f"Done! Results saved to {output_file}") 