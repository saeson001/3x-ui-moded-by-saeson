#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
一键同步：将 inbounds（入站节点）中已存在的客户端配置，生成到 3x-ui 的 clients 表 + client_inbounds 关联表。
一个节点对应一个客户端用户，名称为节点名称（email = 节点 remark，重名自动加 -2/-3 后缀）。
凭证沿用节点 settings 里原有的 uuid/password，保证已有客户端的链接不变。
幂等：已有关联的节点自动跳过，可重复执行。
用法：
    python3 sync_clients.py             # 执行
    python3 sync_clients.py --dry-run   # 只预览不写入
    python3 sync_clients.py --db /path/to/x-ui.db   # 指定数据库
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

    # 表不存在时自动创建（3x-ui 正常启动后已存在，这里兜底）
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

    used = {r["email"] for r in conn.execute("SELECT email FROM clients")}
    rows = conn.execute(
        "SELECT id, remark, tag, protocol, settings FROM inbounds ORDER BY id"
    ).fetchall()
    created = skipped = 0

    for ib in rows:
        ib_id, remark, tag, protocol = ib["id"], ib["remark"], ib["tag"], ib["protocol"]
        try:
            settings = json.loads(ib["settings"] or "{}")
        except Exception:
            settings = {}

        # 幂等：该节点已有客户端关联则跳过
        cnt = conn.execute(
            "SELECT COUNT(*) FROM client_inbounds WHERE inbound_id=?", (ib_id,)
        ).fetchone()[0]
        if cnt > 0:
            print(f"[跳过] 节点 {ib_id} {remark} 已关联 {cnt} 个客户端")
            skipped += 1
            continue

        proto = (protocol or "").upper()
        clients = settings.get("clients") or []
        c = clients[0] if clients else {}
        if not isinstance(c, dict):
            c = {}

        uuid_val = c.get("id") or c.get("uuid") or ""
        password = c.get("password") or ""
        flow = c.get("flow") or ""

        if proto in ("VMESS", "VLESS"):
            if not uuid_val:
                uuid_val = str(uuid.uuid4())
        elif proto == "TROJAN":
            if not password:
                password = str(uuid.uuid4()).replace("-", "")
        elif proto == "SHADOWSOCKS":
            password = settings.get("password") or password or str(uuid.uuid4()).replace("-", "")
        else:
            if not uuid_val:
                uuid_val = str(uuid.uuid4())

        # email = 节点名，重名加后缀
        email = (remark or "").strip() or f"node-{tag or ib_id}"
        base, i = email, 2
        while email in used:
            email = f"{base}-{i}"
            i += 1

        if not DRY_RUN:
            conn.execute(
                """INSERT INTO clients
                   (email, sub_id, uuid, password, flow, enable, comment, created_at, updated_at)
                   VALUES (?,?,?,?,?,1,?,?,?)""",
                (email, str(uuid.uuid4()), uuid_val, password, flow, remark, now, now),
            )
            client_id = conn.execute("SELECT last_insert_rowid()").fetchone()[0]
            conn.execute(
                "INSERT INTO client_inbounds (client_id, inbound_id, flow_override, created_at) VALUES (?,?,?,?)",
                (client_id, ib_id, "", now),
            )
        cred = (uuid_val or password)[:8]
        print(f"[创建] 节点 {ib_id} {remark} ({proto}) -> 客户端 {email} ({cred}...)")
        used.add(email)
        created += 1

    if not DRY_RUN:
        conn.commit()
    conn.close()
    print(f"\n完成: 新建 {created} 个客户端, 跳过 {skipped} 个" + (" (dry-run 未写入)" if DRY_RUN else ""))


if __name__ == "__main__":
    main()
