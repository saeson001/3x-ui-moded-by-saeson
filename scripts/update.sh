#!/bin/bash
#===========================================================================
# 3x-ui moded by saeson — 更新脚本
#===========================================================================

set -e

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

xui_folder="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
REPO_OWNER="saeson001"
REPO_NAME="3x-ui-moded-by-saeson"

[[ $EUID -ne 0 ]] && echo -e "${red}请使用 root 权限运行${plain}" && exit 1

echo -e "${green}========================================${plain}"
echo -e "${green}  3x-ui moded by saeson — 更新工具${plain}"
echo -e "${green}========================================${plain}"
echo ""

# 检测当前版本
CURRENT_VERSION="unknown"
if [[ -f "${xui_folder}/x-ui" ]]; then
    CURRENT_VERSION=$(curl -s http://localhost:2053/api/version 2>/dev/null || echo "unknown")
fi
echo -e "当前版本: ${yellow}${CURRENT_VERSION}${plain}"

# 备份
echo -e "${green}备份当前配置...${plain}"
BACKUP_DIR="/root/x-ui-backup/$(date +%Y%m%d-%H%M%S)"
mkdir -p "${BACKUP_DIR}"

cp /etc/x-ui/x-ui.db "${BACKUP_DIR}/x-ui.db" 2>/dev/null || true
cp ${xui_folder}/bin/config.json "${BACKUP_DIR}/config.json" 2>/dev/null || true
echo -e "备份保存在: ${BACKUP_DIR}"

# 停止面板
echo -e "${yellow}停止面板服务...${plain}"
systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true

# 下载并执行最新安装脚本
echo -e "${green}下载最新版本...${plain}"
curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/install.sh" -o /tmp/install-x-ui.sh

if [[ ! -f /tmp/install-x-ui.sh ]]; then
    echo -e "${red}下载失败，请检查网络连接。${plain}"
    echo -e "${yellow}正在恢复备份...${plain}"
    cp "${BACKUP_DIR}/x-ui.db" /etc/x-ui/x-ui.db 2>/dev/null || true
    cp "${BACKUP_DIR}/config.json" ${xui_folder}/bin/config.json 2>/dev/null || true
    systemctl start x-ui 2>/dev/null || rc-service x-ui start 2>/dev/null || true
    exit 1
fi

bash /tmp/install-x-ui.sh "$@"
rm -f /tmp/install-x-ui.sh

echo -e "${green}更新完成！${plain}"
