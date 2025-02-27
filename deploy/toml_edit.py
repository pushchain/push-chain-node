#!/usr/bin/env python3
import sys
from tomlkit import parse, dumps

def main():
    if len(sys.argv) != 4:
        print("Usage: toml_edit.py <config_file> <key> <new_value>")
        sys.exit(1)

    filename = sys.argv[1]
    key = sys.argv[2]
    new_value = sys.argv[3]

    try:
        with open(filename, "r") as f:
            content = f.read()
    except FileNotFoundError:
        print(f"Error: File {filename} not found.")
        sys.exit(1)

    doc = parse(content)

    if key not in doc:
        print(f"Error: Key '{key}' not found in {filename}.")
        sys.exit(1)

    doc[key] = new_value

    with open(filename, "w") as f:
        f.write(dumps(doc))

    print(f"Updated '{key}' to '{new_value}' in {filename}")

if __name__ == "__main__":
    main()
