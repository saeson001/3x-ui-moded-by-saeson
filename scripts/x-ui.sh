#!/bin/bash
#===========================================================================
# 3x-ui moded by saeson — 管理菜单脚本
#===========================================================================

red='\033[0;31m'
green='\033[0;32m'
blue='\033[0;34m'
yellow='\033[0;33m'
plain='\033[0m'

xui_folder="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
MOD_VERSION="v1.0.0"
REPO_OWNER="saeson001"
REPO_NAME="3x-ui-moded-by-saeson"

# --- 检查 root ---
[[ $EUID -ne 0 ]] && echo -e "${red}请使用 root 权限运行此命令${plain}" && exit 1

# --- 状态检查 ---
check_status() {
    if [[ -f /etc/systemd/system/x-ui.service ]] || [[ -f /etc/init.d/x-ui ]]; then
        if systemctl is-active --quiet x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null | grep -q started; then
            echo -e "面板状态: ${green}运行中${plain}"
        else
            echo -e "面板状态: ${red}已停止${plain}"
        fi
    else
        echo -e "面板状态: ${red}未安装${plain}"
    fi
}

# --- 版本显示 ---
show_version() {
    echo -e "${blue}========================================${plain}"
    echo -e "${blue}   3x-ui moded by saeson${plain}"
    echo -e "${blue}   版本: ${MOD_VERSION}${plain}"
    echo -e "${blue}   修复: External Proxy + REALITY Bug${plain}"
    echo -e "${blue}   仓库: github.com/${REPO_OWNER}/${REPO_NAME}${plain}"
    echo -e "${blue}========================================${plain}"
}

# --- 显示安装结果 ---
show_credentials() {
    local result_file="/etc/x-ui/install-result.env"
    if [[ -f "${result_file}" ]]; then
        echo -e "${green}============ 面板登录信息 ============${plain}"
        source "${result_file}"
        echo -e "  面板地址: ${blue}${XUI_ACCESS_URL}${plain}"
        echo -e "  用户名:   ${yellow}${XUI_USERNAME}${plain}"
        echo -e "  密码:     ${yellow}${XUI_PASSWORD}${plain}"
        echo -e "${green}=====================================${plain}"
    else
        echo -e "${yellow}未找到安装信息，请查看面板设置。${plain}"
    fi
}

# --- 更新面板 ---
update_panel() {
    echo -e "${green}正在更新 3x-ui moded by saeson...${plain}"

    local update_script="/tmp/x-ui-update.sh"
    curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/update.sh" -o "${update_script}" 2>/dev/null

    if [[ -f "${update_script}" ]]; then
        bash "${update_script}"
        rm -f "${update_script}"
    else
        # 内联更新逻辑
        echo -e "${yellow}获取更新脚本失败，使用内置更新流程...${plain}"

        # 停止面板
        systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true

        # 备份
        cp ${xui_folder}/bin/config.json /tmp/config.json.bak 2>/dev/null || true
        cp /etc/x-ui/x-ui.db /tmp/x-ui.db.bak 2>/dev/null || true

        # 重新运行安装脚本
        bash <(curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/install.sh")

        echo -e "${green}更新完成！${plain}"
    fi
}

# --- 卸载面板 ---
uninstall_panel() {
    echo -e "${red}警告: 此操作将删除 3x-ui 及所有配置！${plain}"
    echo -e "${red}数据库文件 (/etc/x-ui/x-ui.db) 将被保留在 /root/x-ui-backup/ 目录${plain}"
    echo ""
    read -rp "确认卸载？(输入 yes 确认): " confirm

    if [[ "${confirm}" != "yes" ]]; then
        echo -e "${yellow}已取消卸载。${plain}"
        return
    fi

    echo -e "${yellow}开始卸载...${plain}"

    # 备份数据库
    local backup_dir="/root/x-ui-backup"
    mkdir -p "${backup_dir}"
    cp /etc/x-ui/x-ui.db "${backup_dir}/x-ui.db.$(date +%Y%m%d%H%M%S).bak" 2>/dev/null || true
    cp ${xui_folder}/bin/config.json "${backup_dir}/config.json.$(date +%Y%m%d%H%M%S).bak" 2>/dev/null || true

    # 停止并移除服务
    systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true
    systemctl disable x-ui 2>/dev/null || rc-update del x-ui 2>/dev/null || true
    rm -f /etc/systemd/system/x-ui.service /etc/init.d/x-ui
    systemctl daemon-reload 2>/dev/null || true

    # 删除面板文件
    rm -rf ${xui_folder}
    rm -f /usr/bin/x-ui

    # 删除 cron 任务
    crontab -l 2>/dev/null | grep -v "fix-reality.sh" | crontab - 2>/dev/null || true

    echo -e "${green}3x-ui moded by saeson 已卸载。${plain}"
    echo -e "${yellow}配置文件备份在: ${backup_dir}${plain}"
}

# --- 应用 Reality 修复 ---
apply_reality_fix() {
    local fix_script="${xui_folder}/fix-reality.sh"
    if [[ -f "${fix_script}" ]]; then
        bash "${fix_script}" fix
        echo -e "${green}Reality 修复已执行。${plain}"
    else
        echo -e "${yellow}修复脚本不存在，请重新安装。${plain}"
    fi
}

# --- 查看 Xray 日志 ---
view_xray_logs() {
    local log_file="${xui_folder}/bin/xray_access.log"
    if [[ -f "${log_file}" ]]; then
        tail -n 50 "${log_file}"
    else
        journalctl -u x-ui --no-pager -n 50
    fi
}

# --- 重启面板 ---
restart_panel() {
    echo -e "${yellow}重启面板...${plain}"
    systemctl restart x-ui 2>/dev/null || rc-service x-ui restart 2>/dev/null
    sleep 2
    if systemctl is-active --quiet x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null | grep -q started; then
        echo -e "${green}面板已重启。${plain}"
    else
        echo -e "${red}面板重启失败，请检查日志。${plain}"
    fi
}

# --- 主菜单 ---
show_menu() {
    clear
    echo -e ""
    echo -e "${blue}================================================${plain}"
    echo -e "${blue}   3x-ui moded by saeson 管理菜单 ${MOD_VERSION}${plain}"
    echo -e "${blue}================================================${plain}"
    check_status
    echo -e ""
    echo -e "${green}---- 面板管理 ----${plain}"
    echo -e "  ${green}1.${plain} 启动面板"
    echo -e "  ${green}2.${plain} 停止面板"
    echo -e "  ${green}3.${plain} 重启面板"
    echo -e "  ${green}4.${plain} 查看状态"
    echo -e ""
    echo -e "${green}---- 账户管理 ----${plain}"
    echo -e "  ${green}5.${plain} 查看登录信息"
    echo -e "  ${green}6.${plain} 重置用户名和密码"
    echo -e ""
    echo -e "${green}---- 系统管理 ----${plain}"
    echo -e "  ${green}7.${plain} 查看面板版本"
    echo -e "  ${green}8.${plain} 更新面板"
    echo -e "  ${green}9.${plain} 应用 Reality 修复"
    echo -e "  ${green}10.${plain} 查看运行日志"
    echo -e ""
    echo -e "${red}---- 危险操作 ----${plain}"
    echo -e "  ${red}11.${plain} 卸载面板"
    echo -e ""
    echo -e "  ${green}0.${plain} 退出"
    echo -e ""
    read -rp "请选择操作 [0-11]: " choice

    case "${choice}" in
        1) systemctl start x-ui 2>/dev/null || rc-service x-ui start 2>/dev/null
           echo -e "${green}面板已启动。${plain}" ;;
        2) systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null
           echo -e "${green}面板已停止。${plain}" ;;
        3) restart_panel ;;
        4) systemctl status x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null ;;
        5) show_credentials ;;
        6)
            local new_user=$(openssl rand -base64 12 | tr -dc 'a-zA-Z0-9' | head -c 8)
            local new_pass=$(openssl rand -base64 16 | tr -dc 'a-zA-Z0-9' | head -c 12)
            ${xui_folder}/x-ui setting -username "${new_user}" >/dev/null 2>&1
            ${xui_folder}/x-ui setting -password "${new_pass}" >/dev/null 2>&1
            echo -e "${green}已重置！${plain}"
            echo -e "  用户名: ${yellow}${new_user}${plain}"
            echo -e "  密码:   ${yellow}${new_pass}${plain}"
            restart_panel
            ;;
        7) show_version ;;
        8) update_panel ;;
        9) apply_reality_fix ;;
        10) view_xray_logs ;;
        11) uninstall_panel ;;
        0) echo -e "${green}再见！${plain}"; exit 0 ;;
        *) echo -e "${red}无效选项${plain}" ;;
    esac

    echo ""
    read -rp "按回车键返回菜单..." _
    show_menu
}

# 处理命令行参数
case "${1:-}" in
    version|--version|-v)
        show_version
        ;;
    update|--update|-u)
        update_panel
        ;;
    uninstall|--uninstall)
        uninstall_panel
        ;;
    fix|--fix)
        apply_reality_fix
        ;;
    status|--status)
        check_status
        systemctl status x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null
        ;;
    restart|--restart)
        restart_panel
        ;;
    info|--info)
        show_credentials
        ;;
    *)
        show_menu
        ;;
esac
