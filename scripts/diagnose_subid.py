#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
诊断 subId 状态：检查入站 JSON、clients 表、client_inbounds 表是否一致。
用法：python3 diagnose_subid.py [--db /path/to/x-ui.db]
"""
import json, sqlite3, sys

DB = "/etc/x-ui/x-ui.db"
args = sys.argv[1:]
if "--db" in args:
    i = args.index("--db")
    if i + 1 < len(args):
        DB = args[i + 1]

db = sqlite3.connect(DB)
db.row_factory = sqlite3.Row

print("=" * 60)
print("1. 统计表记录数")
print("=" * 60)
cnt_clients = db.execute("SELECT COUNT(*) FROM clients").fetchone()[0]
cnt_ci = db.execute("SELECT COUNT(*) FROM client_inbounds").fetchone()[0]
cnt_inbounds = db.execute("SELECT COUNT(*) FROM inbounds").fetchone()[0]
print(f"  inbounds:     {cnt_inbounds}")
print(f"  clients:      {cnt_clients}")
print(f"  client_inbounds: {cnt_ci}")

print("\n" + "=" * 60)
print("2. 检查每个入站的 settings JSON 是否含有 subId")
print("=" * 60)

rows = db.execute(
    "SELECT id, remark, protocol, enable, settings FROM inbounds ORDER BY id"
).fetchall()

has_subid = 0
no_subid = 0
skipped = 0
for r in rows:
    ib_id, remark, proto, enable = r["id"], r["remark"], r["protocol"], r["enable"]
    try:
        s = json.loads(r["settings"] or "{}")
    except Exception:
        s = {}
    c_arr = s.get("clients")
    if not isinstance(c_arr, list) or len(c_arr) == 0:
        print(f"  [跳过] ID={ib_id:3d} {remark:20s} ({proto:12s}) 无 clients 数组")
        skipped += 1
        continue
    first = c_arr[0] if isinstance(c_arr[0], dict) else {}
    sub = first.get("subId") or ""
    cid = first.get("id") or ""
    pw = first.get("password") or ""
    em = first.get("email") or ""
    if sub:
        has_subid += 1
        print(f"  [OK]   ID={ib_id:3d} {remark:20s} ({proto:12s}) subId={sub[:16]}... enable={enable}")
    else:
        no_subid += 1
        cred = (cid or pw or em)[:16]
        print(f"  [缺失] ID={ib_id:3d} {remark:20s} ({proto:12s}) id/pw/em={cred}... enable={enable}")

print(f"\n  汇总: 有subId={has_subid}, 无subId={no_subid}, 跳过={skipped}")

print("\n" + "=" * 60)
print("3. 检查 clients 表 sub_id 分布")
print("=" * 60)
rows = db.execute("SELECT id, email, sub_id, uuid, password FROM clients ORDER BY id").fetchall()
empty_sub = 0
for r in rows:
    sid = r["sub_id"] or ""
    if not sid:
        empty_sub += 1
    print(f"  client id={r['id']:3d} email={r['email']:20s} sub_id={'有' if sid else '空':4s} uuid={str(r['uuid'] or '')[:16]:16s} pw={str(r['password'] or '')[:12]:12s}")

print(f"\n  clients 表中 sub_id 为空的记录: {empty_sub}")

print("\n" + "=" * 60)
print("4. 验证 client_inbounds 关联是否完整")
print("=" * 60)
orphan_clients = db.execute(
    "SELECT c.id, c.email FROM clients c LEFT JOIN client_inbounds ci ON c.id=ci.client_id WHERE ci.client_id IS NULL"
).fetchall()
if orphan_clients:
    print(f"  ⚠️  有 {len(orphan_clients)} 个 clients 未关联任何入站:")
    for r in orphan_clients:
        print(f"     id={r['id']} email={r['email']}")
else:
    print("  ✓ 所有 clients 都有至少一个入站关联")

orphan_inbounds = db.execute(
    "SELECT i.id, i.remark FROM inbounds i LEFT JOIN client_inbounds ci ON i.id=ci.inbound_id WHERE ci.inbound_id IS NULL"
).fetchall()
if orphan_inbounds:
    print(f"  ⚠️  有 {len(orphan_inbounds)} 个入站未关联任何 client:")
    for r in orphan_inbounds:
        print(f"     id={r['id']} remark={r['remark']}")
else:
    print("  ✓ 所有入站都有至少一个 client 关联")

print("\n" + "=" * 60)
print("5. 抽查：随机取 1 个有 subId 的入站，验证 clients.sub_id 是否一致")
print("=" * 60)
row = db.execute(
    "SELECT id, remark, settings FROM inbounds WHERE settings LIKE '%\"subId\"%' LIMIT 1"
).fetchone()
if row:
    s = json.loads(row["settings"] or "{}")
    c_arr = s.get("clients", [])
    if c_arr and isinstance(c_arr[0], dict):
        sub_in_json = c_arr[0].get("subId") or ""
        print(f"  入站 ID={row['id']} remark={row['remark']}")
        print(f"  JSON 中的 subId = {sub_in_json}")
        # 查找关联的 client
        ci = db.execute(
            "SELECT c.id, c.email, c.sub_id FROM clients c JOIN client_inbounds ci ON c.id=ci.client_id WHERE ci.inbound_id=?",
            (row["id"],)
        ).fetchone()
        if ci:
            print(f"  关联 client id={ci['id']} email={ci['email']} sub_id={ci['sub_id']}")
            if ci["sub_id"] == sub_in_json:
                print("  ✓ 一致")
            else:
                print("  ✗ 不一致！")
        else:
            print("  ✗ 未找到关联的 client")
else:
    print("  未找到任何含 subId 的入站")

print("\n" + "=" * 60)
print("诊断完成")
print("=" * 60)
db.close()
