#!/bin/bash
#===========================================================================
# 3x-ui moded by saeson — 卸载脚本
#===========================================================================

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

xui_folder="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"

[[ $EUID -ne 0 ]] && echo -e "${red}请使用 root 权限运行${plain}" && exit 1

echo ""
echo -e "${red}==========================================================${plain}"
echo -e "${red}  警告: 此操作将完全删除 3x-ui moded by saeson${plain}"
echo -e "${red}==========================================================${plain}"
echo ""
echo -e "将删除以下内容:"
echo -e "  - ${xui_folder} (面板文件)"
echo -e "  - /etc/systemd/system/x-ui.service (服务文件)"
echo -e "  - /usr/bin/x-ui (管理命令)"
echo -e "  - 相关的 cron 任务"
echo ""
echo -e "${yellow}数据库和配置文件将备份到 /root/x-ui-backup/${plain}"
echo ""

read -rp "确认卸载？输入 'yes' 确认: " confirm
if [[ "${confirm}" != "yes" ]]; then
    echo -e "${green}已取消。${plain}"
    exit 0
fi

# 备份
BACKUP_DIR="/root/x-ui-backup/backup-$(date +%Y%m%d%H%M%S)"
mkdir -p "${BACKUP_DIR}"
echo -e "${green}备份数据到 ${BACKUP_DIR}...${plain}"
cp /etc/x-ui/x-ui.db "${BACKUP_DIR}/" 2>/dev/null || true
cp ${xui_folder}/bin/config.json "${BACKUP_DIR}/" 2>/dev/null || true
cp /etc/x-ui/install-result.env "${BACKUP_DIR}/" 2>/dev/null || true
cp -r /root/cert "${BACKUP_DIR}/cert" 2>/dev/null || true

# 停止服务
echo -e "${yellow}停止服务...${plain}"
systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true
systemctl disable x-ui 2>/dev/null || rc-update del x-ui 2>/dev/null || true

# 删除文件
echo -e "${yellow}删除面板文件...${plain}"
rm -f /etc/systemd/system/x-ui.service
rm -f /etc/init.d/x-ui
systemctl daemon-reload 2>/dev/null || true

rm -rf ${xui_folder}
rm -f /usr/bin/x-ui

# 删除 cron 任务
echo -e "${yellow}清理 cron 任务...${plain}"
crontab -l 2>/dev/null | grep -v "fix-reality.sh" | crontab - 2>/dev/null || true

# 询问是否删除数据库
read -rp "是否删除数据库文件 /etc/x-ui/？(y/N): " del_db
if [[ "${del_db}" == "y" || "${del_db}" == "Y" ]]; then
    cp -r /etc/x-ui "${BACKUP_DIR}/x-ui-etc" 2>/dev/null || true
    rm -rf /etc/x-ui
    echo -e "${yellow}数据库已删除（备份在 ${BACKUP_DIR}）。${plain}"
else
    echo -e "${green}数据库文件保留在 /etc/x-ui/。${plain}"
fi

echo ""
echo -e "${green}========================================${plain}"
echo -e "${green}  3x-ui moded by saeson 已完全卸载${plain}"
echo -e "${green}  备份位置: ${BACKUP_DIR}${plain}"
echo -e "${green}========================================${plain}"
