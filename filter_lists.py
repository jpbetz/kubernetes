import csv

input_file = "data/api_groups_and_kinds.csv"
temp_file = "data/api_groups_and_kinds_filtered.csv"

# Read the CSV and filter out entries with Kind ending in "List"
filtered_entries = []
with open(input_file, 'r', newline='', encoding='utf-8') as infile:
    reader = csv.DictReader(infile)
    for row in reader:
        if not row["Kind"].endswith("List"):
            filtered_entries.append(row)

# Write the filtered data back to the CSV
with open(input_file, 'w', newline='', encoding='utf-8') as outfile:
    writer = csv.DictWriter(outfile, fieldnames=["APIGroup", "Kind", "ImportPath"])
    writer.writeheader()
    writer.writerows(filtered_entries)

print(f"Removed all *List kinds. {len(filtered_entries)} entries remaining in {input_file}") 