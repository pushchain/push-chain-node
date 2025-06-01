#!/usr/bin/env python3
"""
generate_mnemonics.py

Generate and print 5 BIP-39 (24-word) mnemonics, one per line.
"""

import sys

try:
    from mnemonic import Mnemonic
except ImportError:
    print("The 'mnemonic' package is required. Install with:", file=sys.stderr)
    print("  pip install mnemonic", file=sys.stderr)
    sys.exit(1)

def main():
    mnemo = Mnemonic("english")
    for _ in range(5):
        # Generate a 24-word mnemonic (256 bits of entropy)
        phrase = mnemo.generate(strength=256)
        print(phrase)

if __name__ == "__main__":
    main()
