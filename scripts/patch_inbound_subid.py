#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
回写 subId：把 clients 表的 sub_id 同步到 inbounds.settings.clients[].subId。

背景：3x-ui 生成直链（客户端页二维码/信息弹窗）时，从入站 settings.clients JSON
里按 subId 匹配客户端（internal/sub/service.go matchingClients）。x-ui-yg 迁移
来的客户端 JSON 没有 subId 字段，导致 subLinks 接口返回空 → 弹窗只有订阅链接、
没有 vless/vmess/trojan 直链。本脚本把 clients 表已生成的 sub_id 回写入站 JSON。

匹配策略（入站 JSON 客户端 -> clients 表记录）：
  1. vless/vmess:   client.id == clients.uuid
  2. trojan:        client.password == clients.password
  3. 兜底:          client.email == clients.email
匹配不到（如单节点多客户端未同步全）时自动新建 clients 记录 + client_inbounds 关联。

用法：
    python3 patch_inbound_subid.py             # 执行
    python3 patch_inbound_subid.py --dry-run   # 只预览不写入
    python3 patch_inbound_subid.py --db /path  # 指定数据库
执行后必须重启 x-ui（systemctl restart x-ui）使内存中的入站配置刷新。
"""
import json
import sqlite3
import sys
import time
import uuid

DEFAULT_DB = "/etc/x-ui/x-ui.db"
DRY_RUN = "--dry-run" in sys.argv


def db_path():
    args = sys.argv[1:]
    if "--db" in args:
        i = args.index("--db")
        if i + 1 < len(args):
            return args[i + 1]
    return DEFAULT_DB


def main():
    conn = sqlite3.connect(db_path())
    conn.row_factory = sqlite3.Row
    now = int(time.time() * 1000)

    # 兜底建表（3x-ui 正常启动后已存在）
    conn.execute(
        """CREATE TABLE IF NOT EXISTS clients (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            email TEXT UNIQUE NOT NULL,
            sub_id TEXT, uuid TEXT, password TEXT, auth TEXT, flow TEXT, security TEXT,
            reverse TEXT, wg_private_key TEXT, wg_public_key TEXT, wg_allowed_ips TEXT,
            wg_pre_shared_key TEXT, wg_keep_alive INTEGER DEFAULT 0,
            limit_ip INTEGER, total_gb INTEGER, expiry_time INTEGER,
            enable BOOLEAN DEFAULT 1, tg_id INTEGER, group_name TEXT DEFAULT '',
            comment TEXT, reset INTEGER DEFAULT 0, created_at INTEGER, updated_at INTEGER
        )"""
    )
    conn.execute(
        """CREATE TABLE IF NOT EXISTS client_inbounds (
            client_id INTEGER, inbound_id INTEGER, flow_override TEXT, created_at INTEGER,
            PRIMARY KEY (client_id, inbound_id)
        )"""
    )

    # 索引 clients 表
    clients = conn.execute(
        "SELECT id, email, sub_id, uuid, password FROM clients"
    ).fetchall()
    by_uuid = {}
    by_pw = {}
    by_email = {}
    used_emails = set()
    used_subids = set()
    for c in clients:
        if c["uuid"]:
            by_uuid[c["uuid"]] = c
        if c["password"]:
            by_pw[c["password"]] = c
        if c["email"]:
            by_email[c["email"]] = c
            used_emails.add(c["email"])
        if c["sub_id"]:
            used_subids.add(c["sub_id"])

    rows = conn.execute(
        "SELECT id, remark, tag, protocol, settings FROM inbounds ORDER BY id"
    ).fetchall()
    patched = skipped = created = no_clients = 0

    for ib in rows:
        ib_id, remark, tag, protocol = ib["id"], ib["remark"], ib["tag"], ib["protocol"]
        try:
            settings = json.loads(ib["settings"] or "{}")
        except Exception:
            settings = {}
        c_arr = settings.get("clients")
        if not isinstance(c_arr, list) or len(c_arr) == 0:
            print(f"[跳过] 节点 {ib_id} {remark} ({protocol}): settings 无 clients 数组（ss/hysteria 等无需处理）")
            no_clients += 1
            continue

        changed = False
        for c in c_arr:
            if not isinstance(c, dict):
                continue
            if c.get("subId"):
                continue  # 已有 subId，幂等跳过

            cid = c.get("id") or ""
            pw = c.get("password") or ""
            em = (c.get("email") or "").strip()
            flow = c.get("flow") or ""

            rec = None
            if cid and cid in by_uuid:
                rec = by_uuid[cid]
            elif pw and pw in by_pw:
                rec = by_pw[pw]
            elif em and em in by_email:
                rec = by_email[em]

            if rec and rec["sub_id"]:
                sid = rec["sub_id"]
                action = "回写"
            else:
                # 无对应记录或记录无 sub_id：新建/补全 clients 记录
                email = em or (remark or "").strip() or f"node-{tag or ib_id}"
                base, i = email, 2
                while email in used_emails:
                    email = f"{base}-{i}"
                    i += 1
                sid = str(uuid.uuid4())
                while sid in used_subids:
                    sid = str(uuid.uuid4())
                if rec:  # 记录存在但 sub_id 为空
                    if not DRY_RUN:
                        conn.execute(
                            "UPDATE clients SET sub_id=?, updated_at=? WHERE id=?",
                            (sid, now, rec["id"]),
                        )
                    action = "补 sub_id"
                else:  # 全新记录
                    if not DRY_RUN:
                        conn.execute(
                            """INSERT INTO clients
                               (email, sub_id, uuid, password, flow, enable, comment, created_at, updated_at)
                               VALUES (?,?,?,?,?,1,?,?,?)""",
                            (email, sid, cid, pw, flow, remark, now, now),
                        )
                        client_id = conn.execute("SELECT last_insert_rowid()").fetchone()[0]
                        conn.execute(
                            "INSERT INTO client_inbounds (client_id, inbound_id, flow_override, created_at) VALUES (?,?,?,?)",
                            (client_id, ib_id, "", now),
                        )
                    used_emails.add(email)
                    created += 1
                    action = "新建客户端"
                used_subids.add(sid)

            c["subId"] = sid
            changed = True
            cred = (cid or pw or em)[:8]
            print(f"[{action}] 节点 {ib_id} {remark} -> 客户端 {em or remark} ({cred}...) subId={sid[:8]}")

        if changed:
            settings["clients"] = c_arr
            new_settings = json.dumps(settings, ensure_ascii=False, separators=(",", ":"))
            if not DRY_RUN:
                conn.execute(
                    "UPDATE inbounds SET settings=? WHERE id=?", (new_settings, ib_id)
                )
            patched += 1

    if not DRY_RUN:
        conn.commit()
    conn.close()
    print(
        f"\n完成: 更新入站 {patched} 个, 新建客户端 {created} 个, 跳过 {skipped + no_clients} 个"
        + (" (dry-run 未写入)" if DRY_RUN else "")
    )
    print("提醒: 执行后请运行 systemctl restart x-ui 使配置生效。")


if __name__ == "__main__":
    main()
