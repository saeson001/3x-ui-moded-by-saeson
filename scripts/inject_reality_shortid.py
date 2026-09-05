#!/usr/bin/env python3
"""
inject_reality_shortid.py — Inject the normalizeRealityShortIds call into
the 3x-ui inbound.go source file.

This script finds the normalizeStreamSettings function (which is invoked by both
AddInbound and UpdateInbound) and inserts a call to
s.normalizeRealityShortIds(inbound) right before its closing brace, so every
saved VLESS+reality inbound gets its shortIds normalized to 8 bytes (16 hex
chars). That keeps Clash Meta (Android) subscriptions valid, which otherwise
reject shorter shortIds with "invalid REALITY short ID".

Usage:
    python3 inject_reality_shortid.py <path_to_inbound.go>
"""

import os
import sys


def find_func_end(lines, sig):
    """Return the index of the closing brace of the function whose first line
    starts with `sig`, using brace matching."""
    for i, line in enumerate(lines):
        if line.lstrip().startswith(sig):
            brace = 0
            started = False
            j = i
            while j < len(lines):
                for ch in lines[j]:
                    if ch == '{':
                        brace += 1
                        started = True
                    elif ch == '}':
                        brace -= 1
                        if started and brace == 0:
                            return j
                j += 1
    return None


def main():
    if len(sys.argv) < 2:
        print("Usage: inject_reality_shortid.py <inbound.go path>")
        sys.exit(1)

    path = sys.argv[1]
    if not os.path.exists(path):
        print(f"Error: {path} not found")
        sys.exit(1)

    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
        lines = content.splitlines(keepends=True)

    if 'normalizeRealityShortIds' in content:
        print("Fix already applied (normalizeRealityShortIds call found). Skipping.")
        sys.exit(0)

    sig = "func (s *InboundService) normalizeStreamSettings(inbound *model.Inbound) {"
    end = find_func_end(lines, sig)
    if end is None:
        print("Error: could not find normalizeStreamSettings in inbound.go")
        print("The file may have changed. Please update inject_reality_shortid.py.")
        sys.exit(1)

    indent = '\t'
    call = (
        f'{indent}// Fix: normalize REALITY shortIds to 8 bytes so Clash Meta\n'
        f'{indent}// (Android) subscriptions validate (they require exactly 8-byte short IDs).\n'
        f'{indent}s.normalizeRealityShortIds(inbound)\n'
    )

    new_lines = lines[:end] + [call] + lines[end:]
    with open(path, 'w', encoding='utf-8') as f:
        f.writelines(new_lines)

    print(f"Successfully injected normalizeRealityShortIds call before line {end + 1}")
    print(f"File: {path}")


if __name__ == '__main__':
    main()
