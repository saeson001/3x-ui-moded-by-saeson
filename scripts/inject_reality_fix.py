#!/usr/bin/env python3
"""
inject_reality_fix.py — Inject the preserveRealitySettings call into
the 3x-ui inbound.go source file.

This script finds the CreateHostsFromExternalProxy call in AddInbound()
and inserts a call to s.preserveRealitySettings(tx, inbound) right after
the error check block, so that Reality security parameters (publicKey,
fingerprint) are not lost when External Proxy is configured.

Usage:
    python3 inject_reality_fix.py <path_to_inbound.go> <path_to_reality_fix.go>
"""

import os
import re
import sys


def find_create_hosts_block(lines):
    """
    Find lines containing:
        if _, err := database.CreateHostsFromExternalProxy(tx, inbound.Id, inbound.StreamSettings); err != nil {
            return err
        }
    And return (start_idx, end_idx) of that block.
    """
    for i, line in enumerate(lines):
        if "CreateHostsFromExternalProxy" in line and "err != nil" in line:
            # Found the if statement. Find the matching closing brace.
            brace_count = 0
            found_open = False
            j = i
            while j < len(lines):
                for ch in lines[j]:
                    if ch == '{':
                        brace_count += 1
                        found_open = True
                    elif ch == '}':
                        brace_count -= 1
                        if found_open and brace_count == 0:
                            return (i, j)
                j += 1
    return None


def main():
    if len(sys.argv) < 2:
        print("Usage: inject_reality_fix.py <inbound.go path>")
        sys.exit(1)

    inbound_path = sys.argv[1]

    if not os.path.exists(inbound_path):
        print(f"Error: {inbound_path} not found")
        sys.exit(1)

    with open(inbound_path, 'r', encoding='utf-8') as f:
        content = f.read()
        lines = content.splitlines(keepends=True)

    # Find the CreateHostsFromExternalProxy block
    block = find_create_hosts_block(lines)
    if block is None:
        print("Error: Could not find CreateHostsFromExternalProxy call in inbound.go")
        print("The file may have changed. Please update inject_reality_fix.py.")
        sys.exit(1)

    start, end = block
    indent = re.match(r'^(\t+)', lines[start])
    indent_str = indent.group(1) if indent else '\t\t'

    # Insert the preserveRealitySettings call right after the closing brace
    fix_call = f'{indent_str}// Fix: prevent External Proxy from clearing Reality settings\n'
    fix_call += f'{indent_str}if err := s.preserveRealitySettings(tx, inbound); err != nil {{\n'
    fix_call += f'{indent_str}\treturn err\n'
    fix_call += f'{indent_str}}}\n'

    new_lines = lines[:end + 1] + ['\n'] + [fix_call, '\n'] + lines[end + 1:]

    # Check if already patched
    if 'preserveRealitySettings' in content:
        print("Fix already applied (preserveRealitySettings call found). Skipping.")
        sys.exit(0)

    with open(inbound_path, 'w', encoding='utf-8') as f:
        f.writelines(new_lines)

    print(f"Successfully injected preserveRealitySettings call after line {end + 1}")
    print(f"File: {inbound_path}")


if __name__ == '__main__':
    main()
