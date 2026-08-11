#!/bin/bash
#===========================================================================
# 3x-ui moded by saeson — 一键安装脚本
# 基于原版 install.sh，修复 External Proxy + REALITY Bug
# 适用: Debian / Ubuntu / CentOS / RHEL / Fedora / Arch / Alpine / openSUSE
#===========================================================================

set -e

red='\033[0;31m'
green='\033[0;32m'
blue='\033[0;34m'
yellow='\033[0;33m'
plain='\033[0m'

# --- 项目配置 ---
REPO_OWNER="saeson001"
REPO_NAME="3x-ui-moded-by-saeson"
GITHUB_API="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}"
GITHUB_RELEASES="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download"
UPSTREAM_REPO="MHSanaei/3x-ui"
xui_folder="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
xui_service="${XUI_SERVICE:=/etc/systemd/system}"

# --- 版本标签 ---
MOD_VERSION="v1.0.0"

# --- 检查 root ---
[[ $EUID -ne 0 ]] && echo -e "${red}错误: ${plain}请使用 root 权限运行此脚本" && exit 1

# --- 检测系统 ---
if [[ -f /etc/os-release ]]; then
    source /etc/os-release
    release=$ID
elif [[ -f /usr/lib/os-release ]]; then
    source /usr/lib/os-release
    release=$ID
else
    echo -e "${red}无法检测系统类型，请联系作者！${plain}" >&2
    exit 1
fi

# 保存并恢复 VERSION（防止被 /etc/os-release 覆盖）
_SAVED_VERSION="${VERSION:-}"
# 如果被覆盖后恢复
trap '[[ -z "${_SAVED_VERSION}" ]] || VERSION="${_SAVED_VERSION}"' EXIT

echo -e "${blue}================================================${plain}"
echo -e "${blue}   3x-ui moded by saeson — 一键安装脚本 ${MOD_VERSION}${plain}"
echo -e "${blue}================================================${plain}"
echo -e "${green}系统检测: ${release}${plain}"

# --- 架构检测 ---
arch() {
    case "$(uname -m)" in
        x86_64 | x64 | amd64) echo 'amd64' ;;
        i*86 | x86)           echo '386' ;;
        armv8* | arm64 | aarch64) echo 'arm64' ;;
        armv7* | armv7)       echo 'armv7' ;;
        armv6* | armv6)       echo 'armv6' ;;
        armv5* | armv5)       echo 'armv5' ;;
        s390x)                echo 's390x' ;;
        *) echo -e "${red}不支持的 CPU 架构: $(uname -m)${plain}" && exit 1 ;;
    esac
}

XUI_ARCH=$(arch)
echo -e "${green}系统架构: ${XUI_ARCH}${plain}"

# --- 工具函数 ---
is_ipv4() { [[ "$1" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && return 0 || return 1; }
is_ipv6() { [[ "$1" =~ : ]] && return 0 || return 1; }
is_domain() { [[ "$1" =~ ^([A-Za-z0-9](-*[A-Za-z0-9])*\.)+(xn--[a-z0-9]{2,}|[A-Za-z]{2,})$ ]] && return 0 || return 1; }

gen_random_string() {
    local length="$1"
    openssl rand -base64 $((length * 2)) | tr -dc 'a-zA-Z0-9' | head -c "$length"
}

# --- 安装基础依赖 ---
install_base() {
    echo -e "${green}安装基础依赖...${plain}"
    case "${release}" in
        ubuntu | debian | armbian)
            apt-get update -qq && apt-get install -y -q curl wget tar socat openssl cron tzdata ca-certificates
            ;;
        fedora | amzn | virtuozzo | rhel | almalinux | rocky | ol)
            dnf makecache -y && dnf install -y -q curl wget tar socat openssl cronie tzdata ca-certificates
            ;;
        centos)
            if [[ "${VERSION_ID}" =~ ^7 ]]; then
                yum makecache -y && yum install -y curl wget tar socat openssl cronie tzdata ca-certificates
            else
                dnf makecache -y && dnf install -y -q curl wget tar socat openssl cronie tzdata ca-certificates
            fi
            ;;
        arch | manjaro | parch)
            pacman -Sy --noconfirm curl wget tar socat openssl cronie tzdata ca-certificates
            ;;
        opensuse-tumbleweed | opensuse-leap)
            zypper refresh && zypper -q install -y curl wget tar socat openssl cron tzdata ca-certificates
            ;;
        alpine)
            apk update && apk add curl wget tar socat openssl dcron tzdata ca-certificates
            ;;
        *)
            echo -e "${yellow}未识别的系统，尝试使用 apt...${plain}"
            apt-get update -qq && apt-get install -y -q curl wget tar socat openssl cron tzdata ca-certificates
            ;;
    esac
}

# --- 获取服务器公网 IP ---
get_server_ip() {
    local ip=""
    # 尝试多个获取方式
    ip=$(curl -4s https://api.ipify.org 2>/dev/null) || \
    ip=$(curl -4s https://ifconfig.me 2>/dev/null) || \
    ip=$(curl -4s https://icanhazip.com 2>/dev/null) || \
    ip=$(curl -4s https://checkip.amazonaws.com 2>/dev/null)
    echo "${ip}"
}

# --- 下载文件 ---
download_file() {
    local output="$1" url="$2"
    curl -4fsSLo "$output" "$url" || curl -fsSLo "$output" "$url"
}

# --- 获取最新版本号 ---
get_latest_version() {
    local ver=""
    ver=$(curl -4fsSL "${GITHUB_API}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    if [[ -z "${ver}" ]]; then
        # fallback: try upstream for binary
        ver=$(curl -4fsSL "https://api.github.com/repos/${UPSTREAM_REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    fi
    echo "${ver:-latest}"
}

# --- 安装 3x-ui ---
install_xui() {
    local version="${1:-$(get_latest_version)}"
    echo -e "${green}安装 3x-ui ${version}...${plain}"

    # 停止旧服务
    systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true

    # 下载二进制包
    local temp_dir=$(mktemp -d)
    local pkg_url=""
    local download_success=0

    # 优先从我们的 release 下载
    pkg_url="${GITHUB_RELEASES}/${version}/x-ui-linux-${XUI_ARCH}.tar.gz"
    if download_file "${temp_dir}/x-ui.tar.gz" "${pkg_url}"; then
        download_success=1
    fi

    # 如果我们的 release 没有，从上游下载，然后打补丁
    if [[ ${download_success} -eq 0 ]]; then
        pkg_url="https://github.com/${UPSTREAM_REPO}/releases/latest/download/x-ui-linux-${XUI_ARCH}.tar.gz"
        echo -e "${yellow}从上游下载二进制包...${plain}"
        if ! download_file "${temp_dir}/x-ui.tar.gz" "${pkg_url}"; then
            echo -e "${red}下载失败: ${pkg_url}${plain}"
            rm -rf "${temp_dir}"
            exit 1
        fi
        download_success=1
    fi

    # 解压
    cd "${temp_dir}"
    tar zxf x-ui.tar.gz
    chmod +x x-ui/x-ui x-ui/bin/xray-linux-* x-ui/x-ui.sh 2>/dev/null || true

    # 备份旧数据
    if [[ -f ${xui_folder}/bin/config.json ]]; then
        cp ${xui_folder}/bin/config.json ${temp_dir}/config.json.bak 2>/dev/null || true
    fi
    if [[ -f /etc/x-ui/x-ui.db ]]; then
        cp /etc/x-ui/x-ui.db ${temp_dir}/x-ui.db.bak 2>/dev/null || true
    fi

    # 安装
    rm -rf ${xui_folder}/
    mkdir -p ${xui_folder}/bin
    cp -rf x-ui/* ${xui_folder}/

    # 恢复数据
    if [[ -f ${temp_dir}/config.json.bak ]]; then
        cp ${temp_dir}/config.json.bak ${xui_folder}/bin/config.json 2>/dev/null || true
    fi
    if [[ -f ${temp_dir}/x-ui.db.bak ]]; then
        mkdir -p /etc/x-ui
        cp ${temp_dir}/x-ui.db.bak /etc/x-ui/x-ui.db 2>/dev/null || true
    fi

    # 安装管理命令
    cp -f ${xui_folder}/x-ui.sh /usr/bin/x-ui
    chmod +x /usr/bin/x-ui

    # 清理
    rm -rf "${temp_dir}"

    echo -e "${green}3x-ui 安装完成！${plain}"
}

# --- 配置 systemd 服务 ---
setup_service() {
    echo -e "${green}配置 systemd 服务...${plain}"

    # 选择 service 文件
    local svc_src=""
    if [[ "${release}" == "alpine" ]]; then
        svc_src="${xui_folder}/x-ui.rc"
        cp -f "${svc_src}" /etc/init.d/x-ui
        chmod +x /etc/init.d/x-ui
        rc-update add x-ui default 2>/dev/null || true
    else
        case "${release}" in
            arch | manjaro | parch)
                svc_src="${xui_folder}/x-ui.service.arch"
                ;;
            fedora | amzn | rhel | almalinux | rocky | ol | centos)
                svc_src="${xui_folder}/x-ui.service.rhel"
                ;;
            *)
                svc_src="${xui_folder}/x-ui.service.debian"
                ;;
        esac

        if [[ -f "${svc_src}" ]]; then
            cp -f "${svc_src}" "${xui_service}/x-ui.service"
        else
            # 通用 service 模板
            cat > "${xui_service}/x-ui.service" << EOF
[Unit]
Description=3x-ui Panel (moded by saeson)
After=network.target nss-lookup.target

[Service]
Type=simple
User=root
WorkingDirectory=${xui_folder}
ExecStart=${xui_folder}/x-ui
Restart=on-failure
RestartSec=10s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
        fi

        systemctl daemon-reload
        systemctl enable x-ui
    fi
}

# --- 配置面板 ---
configure_panel() {
    local username=$(gen_random_string 8)
    local password=$(gen_random_string 12)
    local port=$((RANDOM % 55535 + 10000))
    local webBasePath=$(gen_random_string 8)
    local server_ip=$(get_server_ip)

    echo -e "${green}配置面板参数...${plain}"

    # 设置面板参数
    ${xui_folder}/x-ui setting -username "${username}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -password "${password}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -port "${port}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -webBasePath "${webBasePath}" >/dev/null 2>&1

    # 启动服务
    if [[ "${release}" == "alpine" ]]; then
        rc-service x-ui start
    else
        systemctl start x-ui
    fi

    # 等待启动
    sleep 2

    # 显示安装信息
    echo ""
    echo -e "${green}================================================${plain}"
    echo -e "${green}          3x-ui moded by saeson 安装完成！${plain}"
    echo -e "${green}================================================${plain}"
    echo -e ""
    echo -e "  面板地址: ${blue}http://${server_ip}:${port}/${webBasePath}${plain}"
    echo -e "  用户名:   ${yellow}${username}${plain}"
    echo -e "  密码:     ${yellow}${password}${plain}"
    echo -e ""
    echo -e "${green}================================================${plain}"
    echo -e "${yellow}请妥善保存以上信息！${plain}"
    echo -e ""
    echo -e "  管理命令:"
    echo -e "    x-ui          打开管理菜单"
    echo -e "    x-ui version  查看版本"
    echo -e "    x-ui update   更新面板"
    echo -e "    x-ui uninstall 卸载面板"
    echo -e ""

    # 保存到文件
    local result_file="/etc/x-ui/install-result.env"
    mkdir -p /etc/x-ui
    cat > "${result_file}" << EOF
XUI_USERNAME=${username}
XUI_PASSWORD=${password}
XUI_PORT=${port}
XUI_WEB_BASE_PATH=${webBasePath}
XUI_ACCESS_URL=http://${server_ip}:${port}/${webBasePath}
XUI_VERSION=${MOD_VERSION}
XUI_SERVER_IP=${server_ip}
EOF
    chmod 600 "${result_file}"

    echo -e "${green}配置信息已保存到: ${result_file}${plain}"
}

# --- 防火墙配置 ---
configure_firewall() {
    local port=$1
    echo -e "${green}配置防火墙...${plain}"

    # 放行面板端口
    if command -v ufw >/dev/null 2>&1; then
        ufw allow "${port}/tcp" >/dev/null 2>&1 || true
    fi
    if command -v firewall-cmd >/dev/null 2>&1; then
        firewall-cmd --permanent --add-port="${port}/tcp" >/dev/null 2>&1 || true
        firewall-cmd --reload >/dev/null 2>&1 || true
    fi
}

# --- 应用 externalProxy Reality 修复 ---
apply_reality_fix() {
    echo -e "${green}应用 External Proxy + REALITY 修复...${plain}"

    # 创建修复脚本
    cat > ${xui_folder}/fix-reality.sh << 'FIXEOF'
#!/bin/bash
# Reality 修复：确保 xray config.json 中的 realitySettings 不被 externalProxy 清空

CONFIG_FILE="/usr/local/x-ui/bin/config.json"
DB_FILE="/etc/x-ui/x-ui.db"

fix_config() {
    if [[ ! -f "${CONFIG_FILE}" ]]; then
        return 0
    fi

    # 使用 python3 修复
    if command -v python3 &>/dev/null; then
        python3 -c "
import json, sqlite3, sys

try:
    with open('${CONFIG_FILE}') as f:
        config = json.load(f)
    db = sqlite3.connect('${DB_FILE}')
    fixed = False

    for inbound in config.get('inbounds', []):
        tag = inbound.get('tag', '')
        if not tag:
            continue

        ss = inbound.get('streamSettings', {})
        if ss.get('security') != 'reality':
            continue

        rs = ss.get('realitySettings', {})
        settings = rs.get('settings', {})
        pubkey = settings.get('publicKey')
        fingerprint = settings.get('fingerprint')

        # 如果 publicKey 或 fingerprint 为 None，从数据库修复
        if pubkey is None or fingerprint is None:
            try:
                row = db.execute(
                    'SELECT stream_settings FROM inbounds WHERE tag = ?', (tag,)
                ).fetchone()
                if row:
                    db_ss = json.loads(row[0])
                    db_rs = db_ss.get('realitySettings', {})
                    db_settings = db_rs.get('settings', {})
                    if db_settings.get('publicKey'):
                        inbound['streamSettings']['realitySettings'] = db_rs
                        fixed = True
                        print(f'[Fix] Repaired realitySettings for inbound: {tag}')
            except Exception as e:
                print(f'[Warn] Failed to fix {tag}: {e}', file=sys.stderr)

    if fixed:
        with open('${CONFIG_FILE}', 'w') as f:
            json.dump(config, f, indent=2)
        print('[Fix] config.json updated successfully')
    else:
        print('[Fix] No issues found - all realitySettings are intact')

    db.close()
except Exception as e:
    print(f'[Error] {e}', file=sys.stderr)
    sys.exit(0)
"
        return $?
    fi
    return 0
}

# 监控 config.json 变化并自动修复
watch_config() {
    local last_mtime=0
    while true; do
        if [[ -f "${CONFIG_FILE}" ]]; then
            local mtime=$(stat -c %Y "${CONFIG_FILE}" 2>/dev/null || echo 0)
            if [[ ${mtime} -gt ${last_mtime} ]]; then
                last_mtime=${mtime}
                fix_config
                if [[ $? -eq 0 ]]; then
                    # 重载 xray（不重启面板）
                    systemctl reload x-ui 2>/dev/null || true
                fi
            fi
        fi
        sleep 5
    done
}

case "${1:-}" in
    fix) fix_config ;;
    watch) watch_config ;;
    *) fix_config ;;
esac
FIXEOF

    chmod +x ${xui_folder}/fix-reality.sh

    # 执行修复
    ${xui_folder}/fix-reality.sh fix

    # 创建 cron 任务定期修复（每 5 分钟检查一次）
    if ! crontab -l 2>/dev/null | grep -q "fix-reality.sh"; then
        (crontab -l 2>/dev/null; echo "*/5 * * * * ${xui_folder}/fix-reality.sh fix && systemctl reload x-ui 2>/dev/null || true") | crontab -
    fi

    echo -e "${green}External Proxy + REALITY 修复已应用${plain}"
    echo -e "${green}修复脚本位置: ${xui_folder}/fix-reality.sh${plain}"
}

# --- 主流程 ---
main() {
    echo ""
    echo -e "${blue}==================== 开始安装 ====================${plain}"
    echo ""

    # 1. 安装依赖
    install_base

    # 2. 安装 3x-ui
    install_xui "${1:-}"

    # 3. 配置服务
    setup_service

    # 4. 应用 Reality 修复
    apply_reality_fix

    # 5. 获取面板端口
    local panel_port=$(get_latest_version >/dev/null 2>&1; ${xui_folder}/x-ui setting -show true 2>/dev/null | grep 'port:' | awk -F: '{print $2}' | tr -d ' ')
    if [[ -z "${panel_port}" ]]; then
        panel_port=2053
    fi

    # 6. 配置防火墙
    configure_firewall "${panel_port}"

    # 7. 配置面板
    configure_panel

    # 8. 显示完成信息
    echo -e "${green}安装完成！使用 x-ui 命令打开管理菜单。${plain}"
    echo -e "${yellow}提示: 如需 SSL 证书，请运行 x-ui 后选择 SSL 证书管理选项。${plain}"
}

# 如果指定版本参数
VERSION_ARG="${1:-}"

main "${VERSION_ARG}"
