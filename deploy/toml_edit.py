#!/usr/bin/env python3
import sys
from tomlkit import parse, dumps

def update_key(doc, key_path, new_value):
    """
    Update the TOML document by key_path.
    If key_path contains dot(s), interpret it as a dotted path.
    Otherwise, first try the top-level, then search in nested tables.
    """
    # Dotted key path (e.g. "p2p.persistent_peers")
    if '.' in key_path:
        parts = key_path.split('.')
        current = doc
        for part in parts[:-1]:
            if part in current:
                current = current[part]
            else:
                print(f"Error: Section '{part}' not found in the document.")
                sys.exit(1)
        last_key = parts[-1]
        if last_key not in current:
            print(f"Error: Key '{last_key}' not found in section '{'.'.join(parts[:-1])}'.")
            sys.exit(1)
        current[last_key] = new_value
        return

    # Plain key: check at the top level first.
    if key_path in doc:
        doc[key_path] = new_value
        return

    # Search in nested tables (if found exactly once, update it)
    found_tables = []
    for table_key, table_value in doc.items():
        # Check if this is a table (a dict-like container in tomlkit)
        if hasattr(table_value, 'keys') and key_path in table_value:
            found_tables.append(table_value)

    if len(found_tables) == 1:
        found_tables[0][key_path] = new_value
    elif len(found_tables) == 0:
        print(f"Error: Key '{key_path}' not found in the document.")
        sys.exit(1)
    else:
        print(f"Error: Key '{key_path}' found in multiple sections; please use dot notation to specify which one to update.")
        sys.exit(1)

def main():
    if len(sys.argv) != 4:
        print("Usage: toml_edit.py <config_file> <key> <new_value>")
        sys.exit(1)

    filename = sys.argv[1]
    key_path = sys.argv[2]
    new_value = sys.argv[3]

    try:
        with open(filename, "r") as f:
            content = f.read()
    except FileNotFoundError:
        print(f"Error: File {filename} not found.")
        sys.exit(1)

    # Parse the TOML content while preserving formatting and comments
    doc = parse(content)

    # Update the key
    update_key(doc, key_path, new_value)

    # Write the updated TOML back to the file
    with open(filename, "w") as f:
        f.write(dumps(doc))

    print(f"Updated '{key_path}' to '{new_value}' in {filename}")

if __name__ == "__main__":
    main()
