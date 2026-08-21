#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
重建 client_inbounds 关联表（clients <-> inbounds）。

背景：直链生成（客户端页二维码/信息弹窗）走后端 getInboundsBySubId 的三表 JOIN：
  clients -> client_inbounds -> inbounds
x-ui-yg 迁移或官方版覆盖后，client_inbounds 关联常被清空/丢失，导致 JOIN 返回空、
subLinks 接口直链为空（弹窗只剩订阅信息）。clients 表和 inbounds.settings.clients
JSON 里数据都在，只是中间关联断了。本脚本按凭证匹配重建关联：

匹配策略（入站 JSON 客户端 -> clients 表记录）：
  1. vless/vmess:  client.id == clients.uuid
  2. trojan:       client.password == clients.password
  3. 兜底:         client.email == clients.email
匹配不到的打印 [无记录]，可先跑 sync_clients.py 补建客户端记录。
幂等：已存在的关联跳过，可重复执行。

用法：
    python3 fix_client_inbounds.py             # 执行
    python3 fix_client_inbounds.py --dry-run   # 只预览不写入
    python3 fix_client_inbounds.py --db /path  # 指定数据库
只改 client_inbounds 表，xray 配置未变，无需重启 xui（浏览器强刷面板即可）。
"""
import json
import sqlite3
import sys
import time

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

    clients = conn.execute(
        "SELECT id, email, uuid, password FROM clients"
    ).fetchall()
    by_uuid = {c["uuid"]: c for c in clients if c["uuid"]}
    by_pw = {c["password"]: c for c in clients if c["password"]}
    by_email = {c["email"]: c for c in clients if c["email"]}

    added = missing = 0
    for ib in conn.execute(
        "SELECT id, remark, protocol, settings FROM inbounds ORDER BY id"
    ).fetchall():
        try:
            settings = json.loads(ib["settings"] or "{}")
        except Exception:
            continue
        c_arr = settings.get("clients")
        if not isinstance(c_arr, list):
            continue
        for c in c_arr:
            if not isinstance(c, dict):
                continue
            rec = None
            cid = c.get("id") or ""
            pw = c.get("password") or ""
            em = (c.get("email") or "").strip()
            if cid and cid in by_uuid:
                rec = by_uuid[cid]
            elif pw and pw in by_pw:
                rec = by_pw[pw]
            elif em and em in by_email:
                rec = by_email[em]
            if rec is None:
                missing += 1
                print(f"[无记录] 入站 {ib['id']} {ib['remark']}: {em or (cid or pw)[:8]}（可先跑 sync_clients.py）")
                continue
            exists = conn.execute(
                "SELECT 1 FROM client_inbounds WHERE client_id=? AND inbound_id=?",
                (rec["id"], ib["id"]),
            ).fetchone()
            if exists:
                continue
            if not DRY_RUN:
                conn.execute(
                    "INSERT INTO client_inbounds (client_id, inbound_id, flow_override, created_at) VALUES (?,?,?,?)",
                    (rec["id"], ib["id"], "", now),
                )
            added += 1
            print(f"[关联] 客户端 {rec['email']}(id={rec['id']}) <-> 入站 {ib['id']} {ib['remark']} ({ib['protocol']})")

    if not DRY_RUN:
        conn.commit()
    total = conn.execute("SELECT COUNT(*) FROM client_inbounds").fetchone()[0]
    print(
        f"\n完成: 新增关联 {added} 条, 无记录 {missing} 个, 关联表总数 {total}"
        + (" (dry-run 未写入)" if DRY_RUN else "")
    )
    print("只改关联表无需重启 xui，浏览器 Ctrl+Shift+R 强刷面板即可生效。")
    conn.close()


if __name__ == "__main__":
    main()
