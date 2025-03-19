import csv

# Read the CSV file
with open('kubernetes_api_validations_final.csv', 'r') as file:
    reader = csv.reader(file)
    header = next(reader)
    rows = list(reader)
    
    # Custom sorting function that puts "*" entries last
    def custom_sort_key(row):
        # Define priority for API Group (first column)
        if row[0] == "*":
            api_group_priority = 999  # Give wildcard entries lowest priority
        else:
            api_group_priority = 0 if row[0] == "Core" else 1  # Core first, then others
            
        # Return a tuple for sorting
        return (api_group_priority, row[0], row[1], row[2])
    
    # Sort using the custom key function
    sorted_rows = sorted(rows, key=custom_sort_key)
    
# Write the sorted data to a new CSV file
with open('kubernetes_api_validations_sorted.csv', 'w', newline='') as file:
    writer = csv.writer(file)
    writer.writerow(header)
    writer.writerows(sorted_rows)
    
print('File sorted successfully')
print(f'Wrote {len(sorted_rows)} rows to kubernetes_api_validations_sorted.csv')
print('Sorting order: Core API Group first, then alphabetical, with wildcard (*) entries last') 