import re
import sys

def extract_benchmark_data(input_file, output_file):
    # Regular expression to match the benchmark lines and extract both the benchmark name and ns/op value
    pattern = r'BenchmarkValidateUpdate_ChangeAtLeaf/Option:_(\d+)_degree:_(\d+)_fail:_(true|false)_nodeCount:_(\d+)(?:-\d+)?\s+(\d+)\s+([\d.]+)\s+ns/op'
    
    with open(input_file, 'r') as f:
        lines = f.readlines()

    results = []
    for line in lines:
        match = re.search(pattern, line)
        if match:
            option = match.group(1)
            degree = match.group(2)  # degree value
            fail = match.group(3)
            node_count = match.group(4)  # nodeCount value
            cost = match.group(6)  # ns/op value

            # Format: option,degree,fail,nodeCount,cost
            # 'fail' is always false based on the example
            results.append(f"{option},{degree},{fail},{node_count},{cost}")

    # Write the results to the output file
    with open(output_file, 'w') as f:
        f.write('\n'.join(results))
        f.write('\n')  # Add a newline at the end of the file

if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("Usage: python extract_benchmark.py <input_file> <output_file>")
        sys.exit(1)

    input_file = sys.argv[1]
    output_file = sys.argv[2]
    extract_benchmark_data(input_file, output_file)
    print(f"Benchmark data extracted from {input_file} to {output_file}")
