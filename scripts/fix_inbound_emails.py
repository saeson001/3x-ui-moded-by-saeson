#!/usr/bin/env python3
"""
Fix empty email fields in inbound settings JSON (x-ui-yg migration residue).

3x-ui's frontend Zod schema requires email: z.string().min(1), but x-ui-yg
inbounds often have empty email. This causes "Invalid input" on inbound edit.

This script fills empty client emails with the inbound remark (with dedup suffix).
Idempotent: skips clients that already have a non-empty email.
"""
import json, sqlite3, sys, time

DB = "/etc/x-ui/x-ui.db"

def fix_emails(db_path: str, dry_run: bool = False):
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    updated = skipped = 0
    now = int(time.time() * 1000)

    for ib in conn.execute(
        "SELECT id, remark, protocol, settings FROM inbounds ORDER BY id"
    ).fetchall():
        try:
            settings = json.loads(ib["settings"] or "{}")
        except json.JSONDecodeError:
            continue

        clients = settings.get("clients")
        if not isinstance(clients, list) or not clients:
            continue

        changed = False
        seen_emails = set()
        remark = (ib["remark"] or f"inbound-{ib['id']}").strip()

        for idx, c in enumerate(clients):
            if not isinstance(c, dict):
                continue
            email = (c.get("email") or "").strip()
            if email:
                seen_emails.add(email.lower())
                continue

            # Generate unique email based on remark
            base = remark.replace(" ", "_").replace("/", "_")
            candidate = base
            suffix = 2
            while candidate.lower() in seen_emails:
                candidate = f"{base}-{suffix}"
                suffix += 1

            c["email"] = candidate
            seen_emails.add(candidate.lower())
            changed = True
            updated += 1
            action = "[预览]" if dry_run else "[修复]"
            print(f"{action} 入站 {ib['id']} {ib['remark']} 协议={ib['protocol']} 客户端#{idx}: email='' -> '{candidate}'")

        if changed:
            if not dry_run:
                conn.execute(
                    "UPDATE inbounds SET settings = ? WHERE id = ?",
                    (json.dumps(settings, ensure_ascii=False), ib["id"]),
                )
        else:
            skipped += 1

    if not dry_run:
        conn.commit()
    conn.close()

    print(f"\n完成: 修复 {updated} 个空 email, 跳过 {skipped} 个入站")
    if dry_run:
        print("(dry-run 模式，未写入数据库)")
    else:
        print("执行后请运行: systemctl restart x-ui")


if __name__ == "__main__":
    dry = "--dry-run" in sys.argv
    db = sys.argv[sys.argv.index("--db") + 1] if "--db" in sys.argv else DB
    fix_emails(db, dry_run=dry)
