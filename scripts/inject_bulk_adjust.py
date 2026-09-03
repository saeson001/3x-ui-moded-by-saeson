#!/usr/bin/env python3
"""
inject_bulk_adjust.py — Patch upstream client_bulk.go BulkAdjust() so that
"unlimited" (expiry=0 / total=0) clients are no longer skipped when the
admin wants to **set** a positive value.

Upstream behaviour (problematic):
  - ExpiryTime == 0  -> skip with "unlimited expiry"
  - TotalGB   == 0  -> skip with "unlimited traffic"

New behaviour (saeson fix):
  - ExpiryTime == 0 and addDays > 0  -> set to time.Now() + addExpiryMs
  - TotalGB   == 0 and addBytes > 0 -> set to addBytes (absolute)
  - TotalGB   >  0                  -> keep additive (rec.TotalGB + addBytes)
  - Negative adjustments on unlimited -> still skip (cannot reduce from zero)

Usage:
    python3 inject_bulk_adjust.py <build_root>
    <build_root> is the directory containing internal/web/service/client_bulk.go
"""

import os
import re
import sys


def patch_bulk_adjust(filepath):
    """Patch the BulkAdjust function in client_bulk.go."""

    with open(filepath, "r", encoding="utf-8") as f:
        content = f.read()

    # --- Guard: don't patch twice ---
    if "// saeson: allow setting expiry/traffic for unlimited clients" in content:
        print("BulkAdjust already patched. Skipping.")
        return True

    # ================================================================
    # Patch 1: Expiry — when ExpiryTime == 0 and addDays > 0, SET it
    # ================================================================
    # Find the expiry switch block inside the per-email loop.
    # Original:
    #   case rec.ExpiryTime == 0:
    #       if _, exists := skippedReasons[email]; !exists {
    #           skippedReasons[email] = "unlimited expiry"
    #       }
    #
    # Replacement:
    #   case rec.ExpiryTime == 0:
    #       // saeson: allow setting expiry for unlimited clients
    #       if addDays > 0 {
    #           entry.applyExpiry = true
    #           entry.newExpiry = time.Now().UnixMilli() + addExpiryMs
    #       } else {
    #           if _, exists := skippedReasons[email]; !exists {
    #               skippedReasons[email] = "unlimited expiry"
    #           }
    #       }

    old_expiry_block = """\t\t\tcase rec.ExpiryTime == 0:
\t\t\t\tif _, exists := skippedReasons[email]; !exists {
\t\t\t\t\tskippedReasons[email] = "unlimited expiry"
\t\t\t\t}"""

    new_expiry_block = """\t\t\tcase rec.ExpiryTime == 0:
\t\t\t\t// saeson: allow setting expiry for unlimited clients
\t\t\t\tif addDays > 0 {
\t\t\t\t\tentry.applyExpiry = true
\t\t\t\t\tentry.newExpiry = time.Now().UnixMilli() + addExpiryMs
\t\t\t\t} else {
\t\t\t\t\tif _, exists := skippedReasons[email]; !exists {
\t\t\t\t\t\tskippedReasons[email] = "unlimited expiry"
\t\t\t\t\t}
\t\t\t\t}"""

    if old_expiry_block not in content:
        print("ERROR: Could not find the expiry unlimited-skip block.")
        print("The upstream BulkAdjust may have changed. Aborting.")
        return False

    content = content.replace(old_expiry_block, new_expiry_block, 1)
    print("[1/2] Patched expiry block: unlimited clients can now be set with positive days.")

    # ================================================================
    # Patch 2: Traffic — when TotalGB == 0 and addBytes > 0, SET it
    # ================================================================
    # Original:
    #   if rec.TotalGB == 0 {
    #       if _, exists := skippedReasons[email]; !exists {
    #           skippedReasons[email] = "unlimited traffic"
    #       }
    #   } else {
    #       next := max(rec.TotalGB+addBytes, 0)
    #       entry.applyTotal = true
    #       entry.newTotal = next
    #   }
    #
    # Replacement:
    #   if rec.TotalGB == 0 {
    #       // saeson: allow setting traffic limit for unlimited clients
    #       if addBytes > 0 {
    #           entry.applyTotal = true
    #           entry.newTotal = addBytes
    #       } else {
    #           if _, exists := skippedReasons[email]; !exists {
    #               skippedReasons[email] = "unlimited traffic"
    #           }
    #       }
    #   } else {
    #       next := max(rec.TotalGB+addBytes, 0)
    #       entry.applyTotal = true
    #       entry.newTotal = next
    #   }

    old_traffic_block = """\t\t\tif rec.TotalGB == 0 {
\t\t\t\tif _, exists := skippedReasons[email]; !exists {
\t\t\t\t\tskippedReasons[email] = "unlimited traffic"
\t\t\t\t}
\t\t\t} else {
\t\t\t\tnext := max(rec.TotalGB+addBytes, 0)
\t\t\t\tentry.applyTotal = true
\t\t\t\tentry.newTotal = next
\t\t\t}"""

    new_traffic_block = """\t\t\tif rec.TotalGB == 0 {
\t\t\t\t// saeson: allow setting traffic limit for unlimited clients
\t\t\t\tif addBytes > 0 {
\t\t\t\t\tentry.applyTotal = true
\t\t\t\t\tentry.newTotal = addBytes
\t\t\t\t} else {
\t\t\t\t\tif _, exists := skippedReasons[email]; !exists {
\t\t\t\t\t\tskippedReasons[email] = "unlimited traffic"
\t\t\t\t\t}
\t\t\t\t}
\t\t\t} else {
\t\t\t\tnext := max(rec.TotalGB+addBytes, 0)
\t\t\t\tentry.applyTotal = true
\t\t\t\tentry.newTotal = next
\t\t\t}"""

    if old_traffic_block not in content:
        print("ERROR: Could not find the traffic unlimited-skip block.")
        print("The upstream BulkAdjust may have changed. Aborting.")
        return False

    content = content.replace(old_traffic_block, new_traffic_block, 1)
    print("[2/2] Patched traffic block: unlimited clients can now be set with positive bytes.")

    # Add a marker at the top of the function for idempotency
    marker = "\t// saeson: allow setting expiry/traffic for unlimited clients\n"
    # Insert right before the first line of the function body after the signature
    # Find "func (s *ClientService) BulkAdjust(" and insert after the opening brace line
    content = content.replace(
        "func (s *ClientService) BulkAdjust(inboundSvc *InboundService, emails []string, addDays int, addBytes int64, flow string) (BulkAdjustResult, bool, error) {\n\tresult",
        "func (s *ClientService) BulkAdjust(inboundSvc *InboundService, emails []string, addDays int, addBytes int64, flow string) (BulkAdjustResult, bool, error) {\n" + marker + "\tresult",
        1,
    )

    with open(filepath, "w", encoding="utf-8") as f:
        f.write(content)

    print(f"Successfully patched BulkAdjust in {filepath}")
    return True


def main():
    if len(sys.argv) < 2:
        print("Usage: inject_bulk_adjust.py <build_root>")
        sys.exit(1)

    build_root = sys.argv[1]
    target = os.path.join(build_root, "internal", "web", "service", "client_bulk.go")

    if not os.path.exists(target):
        print(f"ERROR: {target} not found")
        sys.exit(1)

    ok = patch_bulk_adjust(target)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
