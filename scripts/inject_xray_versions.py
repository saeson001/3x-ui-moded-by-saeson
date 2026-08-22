#!/usr/bin/env python3
"""
inject_xray_versions.py — Widen the Xray-core version switch list in
3x-ui's server.go (GetXrayVersions).

Upstream v3.4.2 hard-filters the GitHub release list to >= v26.6.27,
so the panel's version switcher only shows ~3 entries. This patch:
  1. Adds ?per_page=100 to the GitHub releases request.
  2. Adds a PublishedAt field to the Release struct.
  3. Replaces the version filter with: every release published within
     the last 365 days (safety floor: major >= 25), so users can roll
     back to e.g. v26.3.27 when a new Xray-core breaks clients.

Usage:
    python3 inject_xray_versions.py internal/web/service/server.go
"""

import os
import sys

MARKER = "saeson mod v1.3.6"

OLD_URL = 'XrayURL    = "https://api.github.com/repos/XTLS/Xray-core/releases"'
NEW_URL = ('XrayURL    = "https://api.github.com/repos/XTLS/Xray-core/releases'
           '?per_page=100"')

OLD_STRUCT = '\tPrerelease      bool   `json:"prerelease"`       // Whether this is a pre-release\n}'
NEW_STRUCT = ('\tPrerelease      bool   `json:"prerelease"`       // Whether this is a pre-release\n'
              '\tPublishedAt     string `json:"published_at"`     // saeson mod v1.3.6: release publish time (RFC3339)\n'
              '}')

OLD_FILTER = ('\t\tmajor, err1 := strconv.Atoi(tagParts[0])\n'
              '\t\tminor, err2 := strconv.Atoi(tagParts[1])\n'
              '\t\tpatch, err3 := strconv.Atoi(tagParts[2])\n'
              '\t\tif err1 != nil || err2 != nil || err3 != nil {\n'
              '\t\t\tcontinue\n'
              '\t\t}\n'
              '\n'
              '\t\tif major > 26 || (major == 26 && minor > 6) || '
              '(major == 26 && minor == 6 && patch >= 27) {\n'
              '\t\t\tversions = append(versions, release.TagName)\n'
              '\t\t}')
NEW_FILTER = ('\t\tmajor, err1 := strconv.Atoi(tagParts[0])\n'
              '\t\tif err1 != nil {\n'
              '\t\t\tcontinue\n'
              '\t\t}\n'
              '\t\tif _, err2 := strconv.Atoi(tagParts[1]); err2 != nil {\n'
              '\t\t\tcontinue\n'
              '\t\t}\n'
              '\t\tif _, err3 := strconv.Atoi(tagParts[2]); err3 != nil {\n'
              '\t\t\tcontinue\n'
              '\t\t}\n'
              '\n'
              '\t\t// saeson mod v1.3.6: upstream v3.4.2 hard-filters to\n'
              '\t\t// >= v26.6.27, leaving only ~3 choices in the switcher.\n'
              '\t\t// Offer every release published within the last 365 days\n'
              '\t\t// instead (v25 safety floor), so users can roll back to\n'
              '\t\t// e.g. v26.3.27 when a new Xray-core breaks clients.\n'
              '\t\tif major < 25 {\n'
              '\t\t\tcontinue\n'
              '\t\t}\n'
              '\t\tif release.PublishedAt != "" {\n'
              '\t\t\tif pubTime, perr := time.Parse(time.RFC3339, release.PublishedAt); perr == nil {\n'
              '\t\t\t\tif time.Since(pubTime) > 365*24*time.Hour {\n'
              '\t\t\t\t\tcontinue\n'
              '\t\t\t\t}\n'
              '\t\t\t}\n'
              '\t\t}\n'
              '\t\tversions = append(versions, release.TagName)')


def main():
    if len(sys.argv) < 2:
        print("Usage: inject_xray_versions.py <server.go path>")
        sys.exit(1)

    path = sys.argv[1]
    if not os.path.exists(path):
        print(f"Error: {path} not found")
        sys.exit(1)

    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()

    if MARKER in content:
        print(f"Already patched ({MARKER} marker found), skipping.")
        return

    for name, old in (("GitHub releases URL", OLD_URL),
                      ("Release struct", OLD_STRUCT),
                      ("version filter block", OLD_FILTER)):
        if old not in content:
            print(f"Error: {name} anchor not found in {path} — "
                  f"upstream source may have changed, aborting.")
            sys.exit(1)

    content = content.replace(OLD_URL, NEW_URL, 1)
    content = content.replace(OLD_STRUCT, NEW_STRUCT, 1)
    content = content.replace(OLD_FILTER, NEW_FILTER, 1)

    # time.Parse needs the time package; upstream server.go already
    # imports it (xrayVersionsCacheTTL), but guard anyway.
    if '"time"' not in content:
        print("Error: time package not imported in server.go, aborting.")
        sys.exit(1)

    with open(path, 'w', encoding='utf-8', newline='\n') as f:
        f.write(content)

    print("Patched GetXrayVersions in", path)
    print("  - releases request: per_page=100")
    print("  - Release struct: + PublishedAt")
    print("  - filter: releases within last 365 days (major >= 25)")


if __name__ == '__main__':
    main()
