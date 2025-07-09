#!/usr/bin/env python3
"""
TOML configuration editor for Push Chain node
"""

import sys
import os
import tomlkit
from pathlib import Path

def load_toml(file_path):
    """Load TOML file and return parsed content"""
    try:
        with open(file_path, 'r') as f:
            return tomlkit.parse(f.read())
    except FileNotFoundError:
        print(f"Error: File {file_path} not found")
        sys.exit(1)
    except Exception as e:
        print(f"Error loading TOML file: {e}")
        sys.exit(1)

def save_toml(file_path, content):
    """Save TOML content to file"""
    try:
        with open(file_path, 'w') as f:
            f.write(tomlkit.dumps(content))
    except Exception as e:
        print(f"Error saving TOML file: {e}")
        sys.exit(1)

def set_nested_value(data, key_path, value):
    """Set a nested value in TOML data using dot notation"""
    keys = key_path.split('.')
    current = data
    
    # Navigate to the parent of the target key
    for key in keys[:-1]:
        if key not in current:
            current[key] = tomlkit.table()
        current = current[key]
    
    # Set the final value
    final_key = keys[-1]
    
    # Handle boolean values
    if isinstance(value, str):
        if value.lower() == 'true':
            value = True
        elif value.lower() == 'false':
            value = False
        elif value.isdigit():
            value = int(value)
        elif value.replace('.', '', 1).isdigit():
            value = float(value)
    
    current[final_key] = value

def main():
    if len(sys.argv) != 4:
        print("Usage: toml_edit.py <file_path> <key_path> <value>")
        print("Example: toml_edit.py config.toml p2p.persistent_peers \"node1@ip:port\"")
        sys.exit(1)
    
    file_path = sys.argv[1]
    key_path = sys.argv[2]
    value = sys.argv[3]
    
    # Check if file exists
    if not os.path.exists(file_path):
        print(f"Error: File {file_path} does not exist")
        sys.exit(1)
    
    # Load, modify, and save
    data = load_toml(file_path)
    set_nested_value(data, key_path, value)
    save_toml(file_path, data)
    
    print(f"Successfully updated {key_path} = {value} in {file_path}")

if __name__ == "__main__":
    main() 