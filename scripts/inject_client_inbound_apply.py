#!/usr/bin/env python3
"""
inject_client_inbound_apply.py — Patch upstream client_inbound_apply.go so that
deleting an inbound (or its clients) does NOT fail with
"invalid clients format in inbound settings" when settings.clients is null,
missing, or not a slice.

Root cause:
  Upstream's delInboundClients() and DelInboundClientByEmail() both do:
    interfaceClients, ok := settings["clients"].([]any)
    if !ok {
        return false, common.NewError("invalid clients format in inbound settings")
    }
  If an inbound's settings JSON has no "clients" key, or "clients": null,
  or "clients" is a non-array type, the delete fails entirely.

Fix:
  Insert a guard before both assertion sites that initialises
  settings["clients"] = []any{} when the existing value is not already a []any.
  This makes deletion resilient to malformed inbound settings.

Usage:
    python3 inject_client_inbound_apply.py <build_root>
"""

import os
import re
import sys


def patch_file(filepath):
    """Patch client_inbound_apply.go at two sites."""

    with open(filepath, "r", encoding="utf-8") as f:
        content = f.read()

    # --- Guard: don't patch twice ---
    if "// saeson: guard nil/missing clients" in content:
        print("client_inbound_apply already patched. Skipping.")
        return True

    count = 0

    # ================================================================
    # Site 1: delInboundClients  (around line 52)
    #   BEFORE: interfaceClients, ok := settings["clients"].([]any)
    #   AFTER:  insert nil-guard that sets clients to empty slice if needed
    # ================================================================
    old_site1 = """\tinterfaceClients, ok := settings["clients"].([]any)
\tif !ok {
\t\treturn false, common.NewError("invalid clients format in inbound settings")
\t}

\ttype removedClient struct {"""

    new_site1 = """\t// saeson: guard nil/missing clients — treat as empty array
\tif _, exists := settings["clients"]; !exists {
\t\tsettings["clients"] = []any{}
\t}
\tif settings["clients"] == nil {
\t\tsettings["clients"] = []any{}
\t}
\tinterfaceClients, ok := settings["clients"].([]any)
\tif !ok {
\t\treturn false, common.NewError("invalid clients format in inbound settings")
\t}

\ttype removedClient struct {"""

    if old_site1 in content:
        content = content.replace(old_site1, new_site1, 1)
        count += 1
        print("[1/2] Patched delInboundClients: added nil/missing clients guard.")
    else:
        print("WARNING: Could not find site 1 (delInboundClients). Upstream may have changed.")

    # ================================================================
    # Site 2: DelInboundClientByEmail  (around line 775)
    #   SAME pattern, same fix
    # ================================================================
    old_site2 = """\tinterfaceClients, ok := settings["clients"].([]any)
\tif !ok {
\t\treturn false, common.NewError("invalid clients format in inbound settings")
\t}

\tvar newClients []any
\tneedApiDel := false
\tfound := false"""

    new_site2 = """\t// saeson: guard nil/missing clients — treat as empty array
\tif _, exists := settings["clients"]; !exists {
\t\tsettings["clients"] = []any{}
\t}
\tif settings["clients"] == nil {
\t\tsettings["clients"] = []any{}
\t}
\tinterfaceClients, ok := settings["clients"].([]any)
\tif !ok {
\t\treturn false, common.NewError("invalid clients format in inbound settings")
\t}

\tvar newClients []any
\tneedApiDel := false
\tfound := false"""

    if old_site2 in content:
        content = content.replace(old_site2, new_site2, 1)
        count += 1
        print("[2/2] Patched DelInboundClientByEmail: added nil/missing clients guard.")
    else:
        print("WARNING: Could not find site 2 (DelInboundClientByEmail). Upstream may have changed.")

    if count == 0:
        print("ERROR: No patches applied. Aborting.")
        return False

    with open(filepath, "w", encoding="utf-8") as f:
        f.write(content)

    print(f"Successfully patched {count} site(s) in {filepath}")
    return True


def main():
    if len(sys.argv) < 2:
        print("Usage: inject_client_inbound_apply.py <build_root>")
        sys.exit(1)

    build_root = sys.argv[1]
    target = os.path.join(build_root, "internal", "web", "service", "client_inbound_apply.go")

    if not os.path.exists(target):
        print(f"ERROR: {target} not found")
        sys.exit(1)

    ok = patch_file(target)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
