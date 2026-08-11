#!/bin/bash
# ============================================================
# fix-reality-config.sh — 修复 3x-ui externalProxy + Reality bug
# 
# 3x-ui 的 externalProxy 功能在生成 xray config.json 时，
# 会错误地清空 Reality 的 publicKey 和 fingerprint，
# 导致 Clash Meta 等客户端无法连接。
#
# 本脚本从数据库读取正确的 Reality 参数，
# 并写入 xray 的 config.json。
#
# 用法:
#   bash fix-reality-config.sh          # 手动执行一次修复
#   bash fix-reality-config.sh --watch  # 安装 systemd path watcher，自动修复
# ============================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 查找数据库和配置文件路径
find_db_config() {
    # 3x-ui 可能的安装路径
    if [ -f "/etc/x-ui/x-ui.db" ]; then
        DB_PATH="/etc/x-ui/x-ui.db"
    elif [ -f "/opt/x-ui/x-ui.db" ]; then
        DB_PATH="/opt/x-ui/x-ui.db"
    else
        DB_PATH=$(find / -name "x-ui.db" -type f 2>/dev/null | head -1)
    fi

    if [ -f "/usr/local/x-ui/bin/config.json" ]; then
        CONFIG_PATH="/usr/local/x-ui/bin/config.json"
    elif [ -f "/opt/x-ui/bin/config.json" ]; then
        CONFIG_PATH="/opt/x-ui/bin/config.json"
    else
        CONFIG_PATH=$(find / -path "*/x-ui/bin/config.json" -type f 2>/dev/null | head -1)
    fi

    if [ -z "$DB_PATH" ] || [ ! -f "$DB_PATH" ]; then
        echo -e "${RED}[ERROR] 找不到 x-ui 数据库文件${NC}"
        exit 1
    fi
    if [ -z "$CONFIG_PATH" ] || [ ! -f "$CONFIG_PATH" ]; then
        echo -e "${RED}[ERROR] 找不到 xray config.json${NC}"
        exit 1
    fi

    echo -e "${GREEN}[INFO] 数据库: $DB_PATH${NC}"
    echo -e "${GREEN}[INFO] 配置文件: $CONFIG_PATH${NC}"
}

# 核心修复逻辑: 从数据库读取 Reality 参数，写入 config.json
do_fix() {
    find_db_config

    python3 -c "
import json
import sqlite3
import os
import shutil

DB_PATH = '$DB_PATH'
CONFIG_PATH = '$CONFIG_PATH'

def url_safe_to_standard(b64):
    '''URL-safe base64 -> standard base64'''
    s = b64.replace('-', '+').replace('_', '/')
    p = 4 - len(s) % 4
    if p != 4:
        s += '=' * p
    return s

db = sqlite3.connect(DB_PATH)

# 获取所有 Reality 入站
rows = db.execute('''
    SELECT port, protocol, tag, stream_settings 
    FROM inbounds 
    WHERE stream_settings LIKE '%\"security\": \"reality\"%'
''').fetchall()

if not rows:
    print('没有找到 Reality 入站，无需修复')
    db.close()
    exit(0)

# 读取 config.json
with open(CONFIG_PATH, 'r') as f:
    config = json.load(f)

fixed_count = 0
needs_fix = []

for row in rows:
    port = row[0]
    protocol = row[1]
    tag = row[2]
    stream_settings = json.loads(row[3])
    reality = stream_settings.get('realitySettings', {})
    db_pubkey = reality.get('settings', {}).get('publicKey', '')
    db_fingerprint = reality.get('settings', {}).get('fingerprint', '')
    db_shortids = reality.get('shortIds', [])
    db_servernames = reality.get('serverNames', [])
    db_privkey = reality.get('privateKey', '')

    if not db_pubkey and not db_fingerprint:
        continue

    # 在 config.json 中找到对应端口
    target = None
    for item in config.get('inbounds', []):
        if item.get('port') == port:
            target = item
            break

    if not target:
        print(f'端口 {port}: config.json 中未找到')
        continue

    cur_rs = target.get('streamSettings', {}).get('realitySettings', {})
    cur_pubkey = cur_rs.get('settings', {}).get('publicKey')
    cur_fingerprint = cur_rs.get('settings', {}).get('fingerprint')

    need_fix_pubkey = not cur_pubkey or cur_pubkey is None
    need_fix_fp = not cur_fingerprint or cur_fingerprint is None

    if not need_fix_pubkey and not need_fix_fp:
        print(f'端口 {port} ({tag}): 无需修复')
        continue

    needs_fix.append(port)
    print(f'端口 {port} ({tag}):', end='')

    if 'streamSettings' not in target:
        target['streamSettings'] = {}

    if 'realitySettings' not in target['streamSettings']:
        target['streamSettings']['realitySettings'] = {}

    rs = target['streamSettings']['realitySettings']

    if 'settings' not in rs:
        rs['settings'] = {}

    if need_fix_pubkey:
        old_val = rs['settings'].get('publicKey', 'None')
        rs['settings']['publicKey'] = db_pubkey
        print(f' publicKey: {old_val} -> {db_pubkey}', end='')

    if need_fix_fp:
        old_val = rs['settings'].get('fingerprint', 'None')
        rs['settings']['fingerprint'] = db_fingerprint
        print(f' fingerprint: {old_val} -> {db_fingerprint}', end='')

    # 确保其他 Reality 字段完整
    if not rs.get('shortIds'):
        rs['shortIds'] = db_shortids
    if not rs.get('serverNames'):
        rs['serverNames'] = db_servernames
    if not rs.get('privateKey'):
        rs['privateKey'] = db_privkey

    fixed_count += 1
    print('')

if needs_fix:
    # 备份
    backup_path = CONFIG_PATH + '.bak.' + str(int(__import__('time').time()))
    shutil.copy2(CONFIG_PATH, backup_path)
    print(f'已备份原配置: {backup_path}')

    # 写入
    with open(CONFIG_PATH, 'w') as f:
        json.dump(config, f, indent=2)
    print(f'已修复 {fixed_count} 个 Reality 入站')
else:
    print('没有需要修复的入站')

db.close()
"
    local rc=$?
    if [ $rc -eq 0 ]; then
        echo -e "${GREEN}[SUCCESS] 修复完成${NC}"
    else
        echo -e "${RED}[ERROR] 修复失败 (exit code: $rc)${NC}"
        return $rc
    fi
}

# 重载 xray 使修复生效
reload_xray() {
    echo -e "${YELLOW}[INFO] 正在重载 xray...${NC}"
    if systemctl is-active --quiet x-ui 2>/dev/null; then
        x-ui restart 2>&1
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}[INFO] x-ui 重启成功 — 但注意 x-ui restart 会重新生成 config.json，可能再次覆盖修复！${NC}"
            echo -e "${YELLOW}[INFO] 建议先执行 fix，再单独重载 xray（不通过 x-ui restart）${NC}"
            # 重新执行一次修复
            echo -e "${YELLOW}[INFO] 正在重新修复（因为 x-ui restart 已覆盖配置）...${NC}"
            do_fix
            # 重载 xray 核心（不影响 config.json）
            if systemctl is-active --quiet xray 2>/dev/null; then
                systemctl reload xray 2>/dev/null || systemctl restart xray 2>/dev/null || true
            fi
            echo -e "${GREEN}[INFO] xray 已重载${NC}"
        else
            echo -e "${RED}[WARN] x-ui 重启失败，配置可能未生效${NC}"
        fi
    else
        echo -e "${YELLOW}[WARN] x-ui 服务未运行${NC}"
    fi
}

# 安装 systemd path watcher
install_watcher() {
    find_db_config

    cat > /etc/systemd/system/x-ui-reality-fix.service << 'SERVICEEOF'
[Unit]
Description=Fix 3x-ui Reality config after x-ui restart
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/fix-reality-config.sh --fix-only
User=root

[Install]
WantedBy=multi-user.target
SERVICEEOF

    cat > /etc/systemd/system/x-ui-reality-fix.path << 'PATHEOF'
[Unit]
Description=Watch x-ui config.json for Reality fix

[Path]
PathChanged=/usr/local/x-ui/bin/config.json

[Install]
WantedBy=multi-user.target
PATHEOF

    # 复制脚本
    cp "$0" /usr/local/bin/fix-reality-config.sh
    chmod +x /usr/local/bin/fix-reality-config.sh

    systemctl daemon-reload
    systemctl enable x-ui-reality-fix.path
    systemctl start x-ui-reality-fix.path

    echo -e "${GREEN}[SUCCESS] systemd watcher 已安装${NC}"
    echo -e "${YELLOW}[INFO] 每次 config.json 变更时自动修复 Reality 配置${NC}"
}

# 卸载 watcher
uninstall_watcher() {
    systemctl stop x-ui-reality-fix.path 2>/dev/null || true
    systemctl disable x-ui-reality-fix.path 2>/dev/null || true
    rm -f /etc/systemd/system/x-ui-reality-fix.service
    rm -f /etc/systemd/system/x-ui-reality-fix.path
    rm -f /usr/local/bin/fix-reality-config.sh
    systemctl daemon-reload
    echo -e "${GREEN}[SUCCESS] watcher 已卸载${NC}"
}

# 仅修复，不做其他操作（供 watcher 调用）
fix_only() {
    do_fix
}

# 查看需要修复的端口
check() {
    find_db_config

    python3 -c "
import json
import sqlite3

db = sqlite3.connect('$DB_PATH')
rows = db.execute('''
    SELECT port, protocol, tag, stream_settings 
    FROM inbounds 
    WHERE stream_settings LIKE '%\"security\": \"reality\"%'
''').fetchall()

if not rows:
    print('没有找到 Reality 入站')
    db.close()
    exit(0)

with open('$CONFIG_PATH', 'r') as f:
    config = json.load(f)

print(f'共 {len(rows)} 个 Reality 入站:\n')

for row in rows:
    port = row[0]
    tag = row[2]
    ss = json.loads(row[3])
    reality = ss.get('realitySettings', {})
    db_pubkey = reality.get('settings', {}).get('publicKey', 'N/A')
    db_fp = reality.get('settings', {}).get('fingerprint', 'N/A')

    # 查 config.json 中的值
    target = None
    for item in config.get('inbounds', []):
        if item.get('port') == port:
            target = item
            break

    if target:
        cur_pubkey = target.get('streamSettings', {}).get('realitySettings', {}).get('settings', {}).get('publicKey', 'N/A')
        cur_fp = target.get('streamSettings', {}).get('realitySettings', {}).get('settings', {}).get('fingerprint', 'N/A')
        pubkey_ok = 'OK' if cur_pubkey and cur_pubkey != 'None' else 'MISSING!'
        fp_ok = 'OK' if cur_fp and cur_fp != 'None' else 'MISSING!'
        print(f'  端口 {port} ({tag})')
        print(f'    数据库 publicKey: {db_pubkey}')
        print(f'    config.json:      {cur_pubkey} [{pubkey_ok}]')
        print(f'    数据库 fingerprint: {db_fp}')
        print(f'    config.json:       {cur_fp} [{fp_ok}]')
        print()
    else:
        print(f'  端口 {port}: config.json 中未找到\n')

db.close()
"
}

# 主入口
case "${1:-}" in
    --check)
        check
        ;;
    --watch)
        install_watcher
        ;;
    --unwatch)
        uninstall_watcher
        ;;
    --fix-only)
        fix_only
        ;;
    --reload)
        do_fix
        reload_xray
        ;;
    *)
        do_fix
        echo ""
        echo -e "${YELLOW}用法:${NC}"
        echo "  $0             手动修复一次"
        echo "  $0 --reload    修复并重载 xray"
        echo "  $0 --check     检查哪些 Reality 入站需要修复"
        echo "  $0 --watch     安装自动修复 watcher（推荐）"
        echo "  $0 --unwatch   卸载自动修复 watcher"
        ;;
esac
