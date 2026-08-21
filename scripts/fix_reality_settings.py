#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
fix_reality_settings.py
修复 x-ui-yg → 3x-ui 迁移时 realitySettings 字段层级不兼容的问题。

x-ui-yg (vaxilu 系) 常把 publicKey/fingerprint 直接放在 realitySettings 顶层：
  realitySettings: { publicKey: "...", fingerprint: "chrome", privateKey: "..." }

3x-ui (MHSanaei) 要求把它们放在 realitySettings.settings 子对象内：
  realitySettings: { serverNames: [...], shortIds: [...], settings: { publicKey: "...", fingerprint: "chrome" } }

本脚本检测并自动迁移顶层字段到 settings 子对象，保证：
  - 面板前端能正确显示公钥
  - 直链/订阅链接能包含 pbk/fp 参数
  - v2rayN/Clash Party 等客户端导入 Reality 节点不报错

用法:
  python3 fix_reality_settings.py [--dry-run] [--db /path/to/x-ui.db]
"""
import json
import sqlite3
import sys

DEFAULT_DB = "/etc/x-ui/x-ui.db"
DRY_RUN = "--dry-run" in sys.argv

def db_path():
    args = sys.argv[1:]
    if "--db" in args:
        i = args.index("--db")
        if i + 1 < len(args):
            return args[i + 1]
    return DEFAULT_DB

def migrate_field(src, dst, key):
    """如果 src 存在 key 且 dst 不存在，则迁移并删除顶层字段。"""
    if key in src and key not in dst:
        dst[key] = src[key]
        del src[key]
        return True
    return False

def main():
    conn = sqlite3.connect(db_path())
    conn.row_factory = sqlite3.Row

    rows = conn.execute(
        "SELECT id, remark, protocol, stream_settings FROM inbounds ORDER BY id"
    ).fetchall()
    fixed = skipped = 0

    for r in rows:
        ib_id = r["id"]
        remark = r["remark"] or ""
        try:
            stream = json.loads(r["stream_settings"] or "{}")
        except Exception:
            stream = {}

        if stream.get("security") != "reality":
            skipped += 1
            continue

        rs = stream.get("realitySettings", {})
        if not rs:
            skipped += 1
            continue

        settings = rs.get("settings")
        if not isinstance(settings, dict):
            settings = {}

        changed = False
        # 迁移顶层字段到 settings
        for key in ("publicKey", "fingerprint", "mldsa65Verify"):
            if migrate_field(rs, settings, key):
                changed = True
                print(f"[迁移] ID={ib_id} {remark}: {key} → realitySettings.settings")

        if changed:
            rs["settings"] = settings
            stream["realitySettings"] = rs
            new_stream = json.dumps(stream, ensure_ascii=False, separators=(",", ":"))
            if not DRY_RUN:
                conn.execute(
                    "UPDATE inbounds SET stream_settings=? WHERE id=?", (new_stream, ib_id)
                )
            fixed += 1
        else:
            skipped += 1

    if not DRY_RUN:
        conn.commit()
    conn.close()
    print(f"\n完成: 修复 {fixed} 个入站, 跳过 {skipped} 个 (无需修改或非reality)" +
          (" (dry-run 未写入)" if DRY_RUN else ""))
    print("提醒: 执行后请运行 systemctl restart x-ui 使配置生效。")

if __name__ == "__main__":
    main()
