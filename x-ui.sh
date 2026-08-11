#!/bin/bash
#===========================================================================
# 3x-ui moded by saeson — 管理命令 (x-ui)
# 安装后位于 /usr/bin/x-ui
#===========================================================================

red='\033[0;31m'
green='\033[0;32m'
blue='\033[0;34m'
yellow='\033[0;33m'
cyan='\033[0;36m'
plain='\033[0m'

xui_folder="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
MOD_VERSION="v1.0.0"
REPO_OWNER="saeson001"
REPO_NAME="3x-ui-moded-by-saeson"
INSTALL_URL="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/install.sh"

# --- 检查 root ---
[[ $EUID -ne 0 ]] && echo -e "${red}请使用 root 权限运行 x-ui${plain}" && exit 1

# --- 获取面板端口 ---
get_panel_port() {
    local port=""
    if [[ -f "${xui_folder}/x-ui" ]]; then
        port=$(${xui_folder}/x-ui setting -show true 2>/dev/null | grep 'port:' | awk -F': ' '{print $2}' | tr -d '[:space:]')
    fi
    echo "${port:-2053}"
}

# --- 获取公网 IP ---
get_server_ip() {
    curl -4s https://api.ipify.org 2>/dev/null || \
    curl -4s https://ifconfig.me 2>/dev/null || \
    curl -4s https://icanhazip.com 2>/dev/null || \
    echo "未知"
}

# --- 获取 webBasePath ---
get_web_base_path() {
    local path=""
    if [[ -f "${xui_folder}/x-ui" ]]; then
        path=$(${xui_folder}/x-ui setting -show true 2>/dev/null | grep 'webBasePath:' | awk -F': ' '{print $2}' | tr -d '[:space:]' | sed 's#^/##')
    fi
    echo "${path:-panel}"
}

# --- 状态检查 ---
check_status() {
    if [[ -f /etc/systemd/system/x-ui.service ]] || [[ -f /etc/init.d/x-ui ]]; then
        if systemctl is-active --quiet x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null | grep -q started; then
            echo -e "状态: ${green}运行中${plain}"
        else
            echo -e "状态: ${red}已停止${plain}"
        fi
    else
        echo -e "状态: ${red}未安装${plain}"
    fi
}

# --- 显示版本 ---
show_version() {
    echo -e "${cyan}========================================${plain}"
    echo -e "${cyan}  3x-ui moded by saeson${plain}"
    echo -e "${cyan}  版本: ${MOD_VERSION}${plain}"
    echo -e "${cyan}  修复: External Proxy + REALITY Bug${plain}"
    echo -e "${cyan}  基于: MHSanaei/3x-ui${plain}"
    echo -e "${cyan}  仓库: github.com/${REPO_OWNER}/${REPO_NAME}${plain}"
    echo -e "${cyan}========================================${plain}"
}

# --- 显示登录信息 ---
show_info() {
    local result_file="/etc/x-ui/install-result.env"
    if [[ -f "${result_file}" ]]; then
        source "${result_file}"
        echo -e "${green}============ 面板登录信息 ============${plain}"
        echo -e "  面板地址: ${blue}${XUI_ACCESS_URL:-未知}${plain}"
        echo -e "  用户名:   ${yellow}${XUI_USERNAME:-未知}${plain}"
        echo -e "  密码:     ${yellow}${XUI_PASSWORD:-未知}${plain}"
        echo -e "  版本:     ${XUI_VERSION:-unknown}${plain}"
        echo -e "${green}=====================================${plain}"
    else
        local ip=$(get_server_ip)
        local port=$(get_panel_port)
        local path=$(get_web_base_path)
        echo -e "${green}面板地址: ${blue}http://${ip}:${port}/${path}${plain}"
        echo -e "${yellow}登录信息请查看面板设置或 /etc/x-ui/install-result.env${plain}"
    fi
}

# --- 重置密码 ---
reset_credentials() {
    local new_user=$(openssl rand -base64 12 2>/dev/null | tr -dc 'a-zA-Z0-9' | head -c 8)
    local new_pass=$(openssl rand -base64 24 2>/dev/null | tr -dc 'a-zA-Z0-9' | head -c 16)
    
    ${xui_folder}/x-ui setting -username "${new_user}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -password "${new_pass}" >/dev/null 2>&1
    
    echo -e "${green}============ 新登录信息 ============${plain}"
    echo -e "  用户名: ${yellow}${new_user}${plain}"
    echo -e "  密码:   ${yellow}${new_pass}${plain}"
    echo -e "${green}=====================================${plain}"
    
    systemctl restart x-ui 2>/dev/null || rc-service x-ui restart 2>/dev/null || true
    echo -e "${green}面板已重启，新密码生效。${plain}"
}

# --- 应用 Reality 修复 ---
do_fix_reality() {
    local fix_script="${xui_folder}/fix-reality.sh"
    if [[ -f "${fix_script}" ]]; then
        bash "${fix_script}" fix
        echo -e "${green}Reality 修复已执行。${plain}"
    else
        echo -e "${yellow}修复脚本不存在。正在重新安装修复...${plain}"
        curl -fsSL "${INSTALL_URL}" | bash -s -- --fix-only 2>/dev/null || {
            echo -e "${red}修复失败，请重新完整安装。${plain}"
            echo -e "  bash <(curl -fsSL ${INSTALL_URL})"
        }
    fi
}

# --- 更新面板 ---
do_update() {
    echo -e "${green}正在更新 3x-ui moded by saeson...${plain}"
    echo ""
    
    # 停止面板
    systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true
    
    # 备份
    mkdir -p /root/x-ui-backup
    cp ${xui_folder}/bin/config.json /root/x-ui-backup/config.json.$(date +%s).bak 2>/dev/null || true
    cp /etc/x-ui/x-ui.db /root/x-ui-backup/x-ui.db.$(date +%s).bak 2>/dev/null || true
    
    # 重新安装
    bash <(curl -fsSL "${INSTALL_URL}")
}

# --- 卸载面板 ---
do_uninstall() {
    echo -e "${red}================================================${plain}"
    echo -e "${red}  警告: 此操作将删除 3x-ui moded by saeson！${plain}"
    echo -e "${red}================================================${plain}"
    echo ""
    
    read -rp "确认卸载？输入 'yes' 确认: " confirm
    if [[ "${confirm}" != "yes" ]]; then
        echo -e "${green}已取消。${plain}"
        return
    fi
    
    local backup_dir="/root/x-ui-backup/$(date +%Y%m%d-%H%M%S)"
    mkdir -p "${backup_dir}"
    
    cp /etc/x-ui/x-ui.db "${backup_dir}/x-ui.db" 2>/dev/null || true
    cp ${xui_folder}/bin/config.json "${backup_dir}/config.json" 2>/dev/null || true
    cp /etc/x-ui/install-result.env "${backup_dir}/" 2>/dev/null || true
    
    systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true
    systemctl disable x-ui 2>/dev/null || rc-update del x-ui 2>/dev/null || true
    
    rm -f /etc/systemd/system/x-ui.service /etc/init.d/x-ui
    systemctl daemon-reload 2>/dev/null || true
    
    rm -rf ${xui_folder}
    rm -f /usr/bin/x-ui
    
    crontab -l 2>/dev/null | grep -v "fix-reality.sh" | crontab - 2>/dev/null || true
    
    echo -e "${green}3x-ui moded by saeson 已卸载。${plain}"
    echo -e "${green}备份位置: ${backup_dir}${plain}"
}

# --- 查看日志 ---
view_logs() {
    echo -e "${cyan}============ 最近 50 行日志 ============${plain}"
    if [[ -f ${xui_folder}/xray_access.log ]]; then
        tail -n 50 ${xui_folder}/xray_access.log 2>/dev/null || \
        tail -n 50 ${xui_folder}/xray_error.log 2>/dev/null
    else
        journalctl -u x-ui --no-pager -n 50 2>/dev/null || \
        journalctl -u xray --no-pager -n 50 2>/dev/null
    fi
    
    echo ""
    echo -e "${yellow}持续监控日志 (按 Ctrl+C 退出):${plain}"
    if [[ -f ${xui_folder}/xray_access.log ]]; then
        tail -f ${xui_folder}/xray_access.log 2>/dev/null
    else
        journalctl -u x-ui --no-pager -f 2>/dev/null
    fi
}

# --- 设置 SSL 证书 ---
setup_ssl() {
    echo -e "${green}==== SSL 证书管理 ====${plain}"
    echo ""
    echo -e "${green}1.${plain} Let's Encrypt 域名证书 (90天)"
    echo -e "${green}2.${plain} Let's Encrypt IP 证书 (6天)"
    echo -e "${green}3.${plain} 自定义证书路径"
    echo -e "${green}0.${plain} 返回"
    echo ""
    read -rp "请选择: " ssl_choice
    
    case "${ssl_choice}" in
        1)
            read -rp "请输入域名: " domain
            if [[ -z "${domain}" ]]; then
                echo -e "${red}域名不能为空${plain}"
                return
            fi
            echo -e "${green}为 ${domain} 申请证书...${plain}"
            
            # 安装 acme.sh
            if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
                curl -s https://get.acme.sh | sh
            fi
            
            systemctl stop x-ui 2>/dev/null || true
            ~/.acme.sh/acme.sh --issue -d "${domain}" --standalone --force
            ~/.acme.sh/acme.sh --installcert -d "${domain}" \
                --key-file /root/cert/${domain}/privkey.pem \
                --fullchain-file /root/cert/${domain}/fullchain.pem
            
            ${xui_folder}/x-ui cert -webCert "/root/cert/${domain}/fullchain.pem" \
                -webCertKey "/root/cert/${domain}/privkey.pem"
            
            systemctl start x-ui 2>/dev/null || true
            echo -e "${green}SSL 证书配置完成！${plain}"
            ;;
        2)
            local ip=$(get_server_ip)
            echo -e "${green}为 IP ${ip} 申请短效证书...${plain}"
            
            if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
                curl -s https://get.acme.sh | sh
            fi
            
            systemctl stop x-ui 2>/dev/null || true
            ~/.acme.sh/acme.sh --issue -d "${ip}" --standalone \
                --server letsencrypt --certificate-profile shortlived --days 6 --force
            
            ~/.acme.sh/acme.sh --installcert -d "${ip}" --force \
                --key-file /root/cert/ip/privkey.pem \
                --fullchain-file /root/cert/ip/fullchain.pem
            
            ${xui_folder}/x-ui cert -webCert "/root/cert/ip/fullchain.pem" \
                -webCertKey "/root/cert/ip/privkey.pem"
            
            systemctl start x-ui 2>/dev/null || true
            echo -e "${green}IP 证书配置完成！${plain}"
            ;;
        3)
            read -rp "证书文件路径 (fullchain.pem): " cert_file
            read -rp "私钥文件路径 (privkey.pem): " key_file
            
            if [[ ! -f "${cert_file}" ]]; then
                echo -e "${red}证书文件不存在: ${cert_file}${plain}"
                return
            fi
            if [[ ! -f "${key_file}" ]]; then
                echo -e "${red}私钥文件不存在: ${key_file}${plain}"
                return
            fi
            
            ${xui_folder}/x-ui cert -webCert "${cert_file}" -webCertKey "${key_file}"
            systemctl restart x-ui 2>/dev/null || true
            echo -e "${green}自定义证书配置完成！${plain}"
            ;;
        *)
            return
            ;;
    esac
}

# --- 防火墙管理 ---
configure_firewall() {
    local port=$(get_panel_port)
    
    echo -e "${green}==== 防火墙配置 ====${plain}"
    echo -e "${green}面板端口: ${port}${plain}"
    echo ""
    
    if command -v ufw &>/dev/null; then
        echo -e "${green}检测到 ufw${plain}"
        read -rp "是否放行端口 ${port}？(Y/n): " confirm
        [[ -z "${confirm}" || "${confirm}" == "y" || "${confirm}" == "Y" ]] && {
            ufw allow "${port}/tcp" && echo -e "${green}已放行端口 ${port}${plain}"
        }
        
        read -rp "是否放行端口 443？(用于 VLESS+TLS) (Y/n): " confirm
        [[ -z "${confirm}" || "${confirm}" == "y" || "${confirm}" == "Y" ]] && {
            ufw allow "443/tcp" && echo -e "${green}已放行端口 443${plain}"
        }
        
        read -rp "是否放行端口 80？(用于 SSL 证书申请) (Y/n): " confirm
        [[ -z "${confirm}" || "${confirm}" == "y" || "${confirm}" == "Y" ]] && {
            ufw allow "80/tcp" && echo -e "${green}已放行端口 80${plain}"
        }
    elif command -v firewall-cmd &>/dev/null; then
        echo -e "${green}检测到 firewalld${plain}"
        firewall-cmd --permanent --add-port="${port}/tcp" 2>/dev/null
        firewall-cmd --reload 2>/dev/null
        echo -e "${green}已放行端口 ${port}${plain}"
    else
        echo -e "${yellow}未检测到防火墙。如果是云服务器，请在安全组中放行端口 ${port}。${plain}"
    fi
}

# --- BBR 加速 ---
enable_bbr() {
    echo -e "${green}开启 BBR 加速...${plain}"
    local bbr_enabled=$(sysctl net.ipv4.tcp_congestion_control 2>/dev/null | awk '{print $3}')
    
    if [[ "${bbr_enabled}" == "bbr" ]]; then
        echo -e "${green}BBR 已启用。${plain}"
        return
    fi
    
    echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf
    echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf
    sysctl -p >/dev/null 2>&1
    
    if lsmod | grep -q bbr; then
        echo -e "${green}BBR 已启用。${plain}"
    else
        modprobe tcp_bbr 2>/dev/null || true
        echo -e "${yellow}BBR 模块已加载，重启后生效。${plain}"
    fi
}

# --- 主菜单 ---
show_menu() {
    clear
    echo ""
    echo -e "${cyan}╔════════════════════════════════════════════════╗${plain}"
    echo -e "${cyan}║       3x-ui moded by saeson ${MOD_VERSION}                 ║${plain}"
    echo -e "${cyan}║       github.com/${REPO_OWNER}/${REPO_NAME}   ║${plain}"
    echo -e "${cyan}╚════════════════════════════════════════════════╝${plain}"
    echo ""
    check_status
    echo ""
    echo -e "${green}  [面板管理]${plain}"
    echo -e "    ${green}1.${plain} 启动面板           ${green}2.${plain} 停止面板"
    echo -e "    ${green}3.${plain} 重启面板           ${green}4.${plain} 查看状态"
    echo ""
    echo -e "${green}  [账户管理]${plain}"
    echo -e "    ${green}5.${plain} 查看登录信息       ${green}6.${plain} 重置用户名密码"
    echo ""
    echo -e "${green}  [系统管理]${plain}"
    echo -e "    ${green}7.${plain} 查看版本           ${green}8.${plain} 更新面板"
    echo -e "    ${green}9.${plain} Reality 修复       ${green}10.${plain} 查看日志"
    echo -e "    ${green}11.${plain} SSL 证书管理       ${green}12.${plain} 防火墙配置"
    echo -e "    ${green}13.${plain} BBR 加速"
    echo ""
    echo -e "${red}  [危险操作]${plain}"
    echo -e "    ${red}14.${plain} 卸载面板"
    echo ""
    echo -e "    ${green}0.${plain} 退出"
    echo ""
    read -rp "请选择 [0-14]: " choice

    case "${choice}" in
        1) systemctl start x-ui 2>/dev/null || rc-service x-ui start 2>/dev/null
           echo -e "${green}面板已启动。${plain}" ;;
        2) systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null
           echo -e "${green}面板已停止。${plain}" ;;
        3) systemctl restart x-ui 2>/dev/null || rc-service x-ui restart 2>/dev/null
           echo -e "${green}面板已重启。${plain}" ;;
        4) systemctl status x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null ;;
        5) show_info ;;
        6) reset_credentials ;;
        7) show_version ;;
        8) do_update ;;
        9) do_fix_reality ;;
        10) view_logs ;;
        11) setup_ssl ;;
        12) configure_firewall ;;
        13) enable_bbr ;;
        14) do_uninstall ;;
        0) echo -e "${green}再见！${plain}"; exit 0 ;;
        *) echo -e "${red}无效选项${plain}" ;;
    esac

    echo ""
    read -rp "按回车键返回菜单..." _
    show_menu
}

# --- 命令行模式 ---
case "${1:-}" in
    version|--version|-v)
        show_version
        ;;
    info|--info|-i)
        show_info
        ;;
    update|--update|-u)
        do_update
        ;;
    uninstall|--uninstall)
        do_uninstall
        ;;
    fix|--fix)
        do_fix_reality
        ;;
    status|--status)
        check_status
        systemctl status x-ui 2>/dev/null || rc-service x-ui status 2>/dev/null
        ;;
    restart|--restart|-r)
        systemctl restart x-ui 2>/dev/null || rc-service x-ui restart 2>/dev/null
        echo -e "${green}面板已重启。${plain}"
        ;;
    start)
        systemctl start x-ui 2>/dev/null || rc-service x-ui start 2>/dev/null
        echo -e "${green}面板已启动。${plain}"
        ;;
    stop)
        systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null
        echo -e "${green}面板已停止。${plain}"
        ;;
    log|logs)
        view_logs
        ;;
    reset)
        reset_credentials
        ;;
    ssl)
        setup_ssl
        ;;
    bbr)
        enable_bbr
        ;;
    help|--help|-h)
        echo "x-ui 管理命令:"
        echo "  x-ui              打开管理菜单"
        echo "  x-ui version      查看版本"
        echo "  x-ui info         查看登录信息"
        echo "  x-ui update       更新面板"
        echo "  x-ui fix          应用 Reality 修复"
        echo "  x-ui status       查看状态"
        echo "  x-ui restart      重启面板"
        echo "  x-ui start        启动面板"
        echo "  x-ui stop         停止面板"
        echo "  x-ui log          查看日志"
        echo "  x-ui reset        重置用户名密码"
        echo "  x-ui ssl          管理 SSL 证书"
        echo "  x-ui bbr          开启 BBR 加速"
        echo "  x-ui uninstall    卸载面板"
        ;;
    *)
        show_menu
        ;;
esac
