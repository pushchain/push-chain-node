#!/usr/bin/env python3
"""
TOML configuration editor for Push Chain validator setup
"""
import sys
import tomlkit

def edit_toml(file_path, key, value):
    """Edit a TOML file with the given key and value"""
    try:
        with open(file_path, 'r') as f:
            doc = tomlkit.parse(f.read())
        
        # Handle nested keys (e.g., "p2p.persistent_peers")
        keys = key.split('.')
        current = doc
        
        for k in keys[:-1]:
            if k not in current:
                current[k] = {}
            current = current[k]
        
        # Set the value
        current[keys[-1]] = value
        
        # Write back
        with open(file_path, 'w') as f:
            f.write(tomlkit.dumps(doc))
        
        print(f"Updated {key} = {value} in {file_path}")
        return True
        
    except Exception as e:
        print(f"Error editing TOML: {e}")
        return False

if __name__ == "__main__":
    if len(sys.argv) != 4:
        print("Usage: python3 toml_edit.py <file_path> <key> <value>")
        sys.exit(1)
    
    file_path = sys.argv[1]
    key = sys.argv[2]
    value = sys.argv[3]
    
    if edit_toml(file_path, key, value):
        sys.exit(0)
    else:
        sys.exit(1)