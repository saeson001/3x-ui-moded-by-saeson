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
xui_folder="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
xui_service="${XUI_SERVICE:=/etc/systemd/system}"

# --- 版本标签 ---
MOD_VERSION="v1.6.0"

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
            apt-get update -qq && apt-get install -y -q curl wget tar socat openssl cron tzdata ca-certificates libsqlite3-0
            ;;
        fedora | amzn | virtuozzo | rhel | almalinux | rocky | ol)
            dnf makecache -y && dnf install -y -q curl wget tar socat openssl cronie tzdata ca-certificates sqlite-libs
            ;;
        centos)
            if [[ "${VERSION_ID}" =~ ^7 ]]; then
                yum makecache -y && yum install -y curl wget tar socat openssl cronie tzdata ca-certificates sqlite-libs
            else
                dnf makecache -y && dnf install -y -q curl wget tar socat openssl cronie tzdata ca-certificates sqlite-libs
            fi
            ;;
        arch | manjaro | parch)
            pacman -Sy --noconfirm curl wget tar socat openssl cronie tzdata ca-certificates sqlite
            ;;
        opensuse-tumbleweed | opensuse-leap)
            zypper refresh && zypper -q install -y curl wget tar socat openssl cron tzdata ca-certificates sqlite3
            ;;
        alpine)
            apk update && apk add curl wget tar socat openssl dcron tzdata ca-certificates sqlite
            ;;
        *)
            echo -e "${yellow}未识别的系统，尝试使用 apt...${plain}"
            apt-get update -qq && apt-get install -y -q curl wget tar socat openssl cron tzdata ca-certificates libsqlite3-0
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
# v1.4.0: 去掉强制 IPv4（-4 会在 IPv6-only 的 VPS 上直接失败）；
#         改为让 curl 自动选择协议，失败时再重试。严禁回退上游。
#   --connect-timeout 15  连接超时 15 秒
#   --speed-limit 1024 --speed-time 60  速度低于 1KB/s 持续 60 秒即中止
#   --retry 3 --retry-delay 2  失败自动重试 3 次
download_file() {
    local output="$1" url="$2"
    curl -fsSLo "$output" \
        --connect-timeout 15 --speed-limit 1024 --speed-time 60 \
        --retry 3 --retry-delay 2 \
        "$url" \
    || curl -fsSLo "$output" \
        --connect-timeout 15 --speed-limit 1024 --speed-time 60 \
        --retry 3 --retry-delay 2 \
        "$url"
}

# --- 获取最新版本号 ---
get_latest_version() {
    local ver=""
    local attempt
    for attempt in 1 2 3; do
        ver=$(curl -fsSL -m 20 "${GITHUB_API}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
        if [[ -n "${ver}" ]]; then
            echo "${ver}"
            return 0
        fi
        echo -e "${yellow}获取最新版本失败（第 ${attempt} 次），重试...${plain}" >&2
        sleep 2
    done
    # 最后兜底：用脚本内置的 MOD_VERSION（保证永远能拿到我们自己的版本）
    echo "${MOD_VERSION}"
}

# --- 安装 3x-ui ---
# 全局标志: 是否使用了我们自己的编译版（Go 源码级修复）
USING_MODDED_BINARY=0

install_xui() {
    local version="${1:-$(get_latest_version)}"
    echo -e "${green}安装 3x-ui ${version}...${plain}"

    # 停止旧服务（并强杀可能残留的旧进程：避免覆盖正在运行的二进制、或旧进程未被
    # systemd 跟踪导致后续 restart 变 no-op，仍用旧二进制吐旧前端）
    systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true
    pkill -9 -f "${xui_folder}/x-ui" 2>/dev/null || true

    # 下载二进制包
    local temp_dir=$(mktemp -d)
    local pkg_url=""
    local download_success=0

    # 只从我们自己的 fork 下载编译版（Go 源码级修复已内置）。
    # ⚠️ 严禁静默回退到上游 MHSanaei/3x-ui：一旦回退会装上官方版，
    #    导致面板变成上游原生布局（用户报告的「更新完还是 3.6 官方版」根因）。
    pkg_url="${GITHUB_RELEASES}/${version}/x-ui-linux-${XUI_ARCH}.tar.gz"
    echo -e "${blue}下载地址: ${pkg_url}${plain}"
    if download_file "${temp_dir}/x-ui.tar.gz" "${pkg_url}"; then
        # 校验文件确实存在且非空（防止 curl 写入空文件却返回 0）
        if [[ -s "${temp_dir}/x-ui.tar.gz" ]]; then
            download_success=1
            USING_MODDED_BINARY=1
            echo -e "${green}使用 saeson 定制版 (批量关联入站 + 流量重置 + Reality 修复)${plain}"
        else
            echo -e "${red}下载文件为空，下载失败${plain}"
        fi
    fi

    if [[ ${download_success} -eq 0 ]]; then
        echo -e "${red}============================================${plain}"
        echo -e "${red}  从 saeson fork 下载编译版失败！${plain}"
        echo -e "${red}  为避免误装上游官方版（会丢失所有 mod 功能），已中止安装。${plain}"
        echo -e "${red}============================================${plain}"
        echo -e "${yellow}排查建议：${plain}"
        echo -e "  1. 测试网络: ${blue}curl -fsSL -I ${pkg_url}${plain}"
        echo -e "  2. 确认仓库 ${REPO_OWNER}/${REPO_NAME} 的 Release 是否已发布对应版本（x-ui-linux-${XUI_ARCH}.tar.gz）"
        echo -e "  3. 若仍拉取失败，手动从 GitHub Releases 下载该文件上传到 VPS，"
        echo -e "     解压后覆盖 /usr/local/x-ui/ 再执行 x-ui 重启"
        rm -rf "${temp_dir}"
        exit 1
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

    # 检查并下载缺失的 geo 数据文件（xray-core 路由必需）
    if [[ ! -f ${xui_folder}/bin/geoip.dat ]] || [[ ! -f ${xui_folder}/bin/geosite.dat ]]; then
        echo -e "${yellow}下载 xray-core geo 数据文件...${plain}"
        local XRAY_VER=$(curl -s -m 15 https://api.github.com/repos/XTLS/Xray-core/releases/latest | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
        if [[ -n "${XRAY_VER}" ]]; then
            # Download and extract just the dat files
            local geo_zip="${temp_dir}/xray-geo.zip"
            local xray_arch_flag="64"
            [[ "${XUI_ARCH}" == "arm64" ]] && xray_arch_flag="arm64-v8a"
            [[ "${XUI_ARCH}" == "armv7" ]] && xray_arch_flag="armv7a"
            curl -fsSL "https://github.com/XTLS/Xray-core/releases/download/${XRAY_VER}/Xray-linux-${xray_arch_flag}.zip" \
                --connect-timeout 15 --speed-limit 1024 --speed-time 60 --retry 2 --retry-delay 2 \
                -o "${geo_zip}" 2>/dev/null
            if [[ -f "${geo_zip}" ]]; then
                unzip -o "${geo_zip}" geoip.dat geosite.dat -d "${xui_folder}/bin/" 2>/dev/null || true
                rm -f "${geo_zip}"
            fi
        fi
        # Verify
        if [[ -f ${xui_folder}/bin/geoip.dat ]] && [[ -f ${xui_folder}/bin/geosite.dat ]]; then
            echo -e "${green}xray-core geo 数据文件已就绪${plain}"
        else
            echo -e "${red}警告: geoip.dat/geosite.dat 下载失败，xray 可能无法正常启动${plain}"
        fi
    fi

    # 恢复数据
    if [[ -f ${temp_dir}/config.json.bak ]]; then
        cp ${temp_dir}/config.json.bak ${xui_folder}/bin/config.json 2>/dev/null || true
    fi
    if [[ -f ${temp_dir}/x-ui.db.bak ]]; then
        mkdir -p /etc/x-ui
        cp ${temp_dir}/x-ui.db.bak /etc/x-ui/x-ui.db 2>/dev/null || true
    fi

    # 安装管理命令（使用我们定制的 x-ui.sh）
    cp -f ${xui_folder}/x-ui.sh /usr/bin/x-ui 2>/dev/null || true
    chmod +x /usr/bin/x-ui 2>/dev/null || true

    # 创建 Clash Link 存储目录
    mkdir -p /etc/x-ui/clash-configs
    chmod 755 /etc/x-ui/clash-configs

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

# --- 启动/重启 x-ui 服务（强制杀掉残留旧进程）---
# v1.4.2: 修复「二进制已更新、浏览器却仍显示旧版」的根因——旧 x-ui 进程可能并非由
# systemd 拉起（或 stop 未匹配到），使 restart 成为 no-op，旧进程继续用内嵌的旧前端服务。
# 这里先按二进制路径强杀任何残留进程，再 restart/start，确保新二进制（含内嵌前端）真正接管。
start_xui_service() {
    pkill -9 -f "${xui_folder}/x-ui" 2>/dev/null || true
    sleep 1
    if [[ "${release}" == "alpine" ]]; then
        rc-service x-ui restart 2>/dev/null || rc-service x-ui start 2>/dev/null || true
    else
        systemctl restart x-ui 2>/dev/null || systemctl start x-ui 2>/dev/null || nohup "${xui_folder}/x-ui" >/dev/null 2>&1 &
    fi
    sleep 2
}

# --- 配置面板 ---
configure_panel() {
    local server_ip=$(get_server_ip)

    # 已有面板数据（更新/重装场景）：保留原有账号/端口/基础路径不变，
    # 绝不随机覆盖（否则用户会被锁在外面找不到新地址）
    if [[ -f /etc/x-ui/x-ui.db ]]; then
        echo -e "${yellow}检测到已有面板数据，保留原有账号/端口/基础路径不变${plain}"

        # 启动服务（强制重启，杀掉残留旧进程）
        start_xui_service

        # 读取现有配置并展示
        local info=$(${xui_folder}/x-ui setting -show true 2>/dev/null || echo "")
        local cur_port=$(echo "$info" | grep -Eo 'port: .+' | awk '{print $2}')
        local cur_path=$(echo "$info" | grep -Eo 'webBasePath: .+' | awk '{print $2}')
        local cur_user=""
        command -v sqlite3 > /dev/null 2>&1 && cur_user=$(sqlite3 /etc/x-ui/x-ui.db "SELECT username FROM users ORDER BY id LIMIT 1;" 2>/dev/null || echo "")

        echo ""
        echo -e "${green}================================================${plain}"
        echo -e "${green}    3x-ui moded by saeson 更新完成！（配置已保留）${plain}"
        echo -e "${green}================================================${plain}"
        echo ""
        echo -e "  面板地址: ${blue}http://${server_ip}:${cur_port:-2053}${cur_path:-/}${plain}"
        echo -e "  用户名:   ${yellow}${cur_user:-admin}${plain}"
        echo -e "  密码:     ${yellow}（沿用原密码，忘记可用 x-ui 菜单选项 7 重置）${plain}"
        echo ""
        echo -e "${green}================================================${plain}"
        return 0
    fi

    # ---- 全新安装：交互式配置 ----
    local username=$(gen_random_string 8)
    local password=$(gen_random_string 12)
    local port=$((RANDOM % 55535 + 10000))
    local webBasePath=$(gen_random_string 8)

    echo -e "${green}配置面板参数...${plain}"

    # 中文交互：是否自定义端口（直接回车 = 使用随机端口）
    echo -e "面板默认使用随机端口 ${yellow}${port}${plain}（更安全，避免被扫描）"
    read -rp "是否自定义面板端口? 直接输入端口号(1-65535)，或直接回车使用上面的随机端口: " custom_port
    custom_port="${custom_port// /}"
    if [[ -n "${custom_port}" ]]; then
        if [[ "${custom_port}" =~ ^[0-9]+$ ]] && [[ "${custom_port}" -ge 1 ]] && [[ "${custom_port}" -le 65535 ]]; then
            port="${custom_port}"
            echo -e "${green}已使用自定义端口: ${port}${plain}"
        else
            echo -e "${yellow}端口无效，仍使用随机端口: ${port}${plain}"
        fi
    fi

    # 设置面板参数
    ${xui_folder}/x-ui setting -username "${username}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -password "${password}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -port "${port}" >/dev/null 2>&1
    ${xui_folder}/x-ui setting -webBasePath "${webBasePath}" >/dev/null 2>&1

    # 启动服务（强制重启，杀掉残留旧进程）
    start_xui_service

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
    echo -e "  Clash 订阅链接:"
    echo -e "    面板 > Clash 配置 > 一键生成"
    echo -e "    API: POST /panel/api/clash-link/generate"
    echo -e "    备份: GET  /panel/api/clash-link/backup"
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
    # 如果使用的是编译修复版，Go 源码已内置修复，无需运行时脚本
    if [[ ${USING_MODDED_BINARY} -eq 1 ]]; then
        echo -e "${green}saeson 定制功能 (Go 源码级): 批量关联入站 / 流量重置 / Reality 修复 / Clash 订阅 — 已内置${plain}"
        echo -e "${green}无需额外运行时脚本。${plain}"
        return 0
    fi

    echo -e "${yellow}应用 External Proxy + REALITY 修复 (运行时脚本)...${plain}"

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

# --- 检查是否已安装 ---
installed_xui_version() {
    if [[ -x "${xui_folder}/x-ui" ]]; then
        ${xui_folder}/x-ui -v 2>/dev/null || echo "unknown"
    else
        echo ""
    fi
}

# --- 卸载 ---
do_uninstall() {
    echo -e "${blue}==================== 卸载 3x-ui ====================${plain}"
    echo ""
    
    if [[ ! -x "${xui_folder}/x-ui" ]]; then
        echo -e "${red}3x-ui 未安装，无需卸载。${plain}"
        return 1
    fi
    
    echo -e "${yellow}以下操作将完全删除 3x-ui 面板及其数据：${plain}"
    echo -e "  - 面板程序: ${xui_folder}"
    echo -e "  - 数据库: /etc/x-ui/x-ui.db"
    echo -e "  - 配置文件: /etc/x-ui/"
    echo ""
    read -rp "确认卸载? [y/N]: " confirm
    if [[ "${confirm}" != "y" && "${confirm}" != "Y" ]]; then
        echo -e "${green}已取消卸载。${plain}"
        return 0
    fi
    
    echo -e "${green}正在卸载 3x-ui...${plain}"
    systemctl stop x-ui 2>/dev/null || true
    systemctl disable x-ui 2>/dev/null || true
    rm -f /etc/systemd/system/x-ui.service
    rm -f /usr/bin/x-ui
    rm -rf "${xui_folder}"
    rm -rf /etc/x-ui
    systemctl daemon-reload 2>/dev/null || true
    
    echo -e "${green}3x-ui 已卸载完成。${plain}"
}

# --- 更新 ---
do_update() {
    echo -e "${blue}==================== 更新 3x-ui ====================${plain}"
    echo ""
    
    if [[ ! -x "${xui_folder}/x-ui" ]]; then
        echo -e "${red}3x-ui 未安装，请先安装。${plain}"
        echo -e "${yellow}提示: 选择选项 1 进行安装。${plain}"
        return 1
    fi
    
    local current_version=$(installed_xui_version)
    local latest_version=$(get_latest_version)
    echo -e "${green}当前面板版本: ${current_version:-unknown} (定制版 ${MOD_VERSION})${plain}"
    echo -e "${green}最新发布版本: ${latest_version}${plain}"
    
    if [[ "${current_version}" == "${latest_version}" ]]; then
        echo -e "${yellow}已经是最新版本，无需更新。${plain}"
        read -rp "是否强制重新安装? [y/N]: " force
        if [[ "${force}" != "y" && "${force}" != "Y" ]]; then
            return 0
        fi
    fi
    
    # 备份数据
    local temp_dir=$(mktemp -d)
    echo -e "${green}备份现有数据...${plain}"
    if [[ -f ${xui_folder}/bin/config.json ]]; then
        cp ${xui_folder}/bin/config.json ${temp_dir}/config.json.bak
    fi
    if [[ -f /etc/x-ui/x-ui.db ]]; then
        cp /etc/x-ui/x-ui.db ${temp_dir}/x-ui.db.bak
    fi
    if [[ -f /etc/x-ui/x-ui.db-shm ]]; then
        cp /etc/x-ui/x-ui.db-shm ${temp_dir}/x-ui.db-shm.bak 2>/dev/null || true
    fi
    if [[ -f /etc/x-ui/x-ui.db-wal ]]; then
        cp /etc/x-ui/x-ui.db-wal ${temp_dir}/x-ui.db-wal.bak 2>/dev/null || true
    fi
    
    # 停止服务
    systemctl stop x-ui 2>/dev/null || rc-service x-ui stop 2>/dev/null || true
    
    # 下载并安装新版本
    install_xui "${latest_version}"
    
    # 恢复数据
    if [[ -f ${temp_dir}/config.json.bak ]]; then
        cp ${temp_dir}/config.json.bak ${xui_folder}/bin/config.json
    fi
    if [[ -f ${temp_dir}/x-ui.db.bak ]]; then
        mkdir -p /etc/x-ui
        cp ${temp_dir}/x-ui.db.bak /etc/x-ui/x-ui.db
    fi
    if [[ -f ${temp_dir}/x-ui.db-shm.bak ]]; then
        cp ${temp_dir}/x-ui.db-shm.bak /etc/x-ui/x-ui.db-shm 2>/dev/null || true
    fi
    if [[ -f ${temp_dir}/x-ui.db-wal.bak ]]; then
        cp ${temp_dir}/x-ui.db-wal.bak /etc/x-ui/x-ui.db-wal 2>/dev/null || true
    fi
    
    # 安装管理命令
    cp -f ${xui_folder}/x-ui.sh /usr/bin/x-ui 2>/dev/null || true
    chmod +x /usr/bin/x-ui 2>/dev/null || true
    
    # 重启服务
    systemctl restart x-ui 2>/dev/null || rc-service x-ui restart 2>/dev/null || true
    sleep 2
    
    rm -rf "${temp_dir}"
    
    echo -e "${green}3x-ui 已更新到 ${latest_version}！${plain}"
    echo -e "${yellow}提示: 运行 x-ui 命令打开管理菜单。${plain}"
}

# --- 查看版本 ---
do_check_version() {
    echo -e "${blue}==================== 版本信息 ====================${plain}"
    echo ""
    
    local current_version=$(installed_xui_version)
    local latest_version=$(get_latest_version)
    
    echo -e "  面板内核版本: ${green}${current_version:-未安装}${plain}"
    echo -e "  定制版版本:   ${green}${MOD_VERSION}${plain}"
    echo -e "  最新发布:     ${green}${latest_version}${plain}"
    echo -e "  仓库:         ${green}${REPO_OWNER}/${REPO_NAME}${plain}"
    echo ""
    
    if [[ -n "${current_version}" && "${current_version}" != "${latest_version}" ]]; then
        echo -e "${yellow}发现新版本，建议运行更新。${plain}"
    elif [[ -n "${current_version}" ]]; then
        echo -e "${green}当前已是最新版本。${plain}"
    fi
}

# --- 交互菜单 ---
show_install_menu() {
    echo ""
    echo -e "${blue}================================================${plain}"
    echo -e "${blue}   3x-ui moded by saeson 一键管理脚本 ${MOD_VERSION}${plain}"
    echo -e "${blue}================================================${plain}"
    echo -e ""
    echo -e "  ${green}1.${plain} 安装 3x-ui"
    echo -e "  ${green}2.${plain} 更新 3x-ui"
    echo -e "  ${green}3.${plain} 查看版本"
    echo -e "  ${green}4.${plain} 卸载 3x-ui"
    echo -e "  ${green}0.${plain} 退出"
    echo -e ""
    echo -e "${blue}================================================${plain}"
    echo ""
    
    read -rp "请选择操作 [0-4]: " choice
    case "${choice}" in
        1)
            if [[ -x "${xui_folder}/x-ui" ]]; then
                echo -e "${yellow}3x-ui 已安装，当前版本: $(installed_xui_version)${plain}"
                read -rp "是否重新安装? (将保留数据) [y/N]: " reinstall
                if [[ "${reinstall}" != "y" && "${reinstall}" != "Y" ]]; then
                    show_install_menu
                    return
                fi
            fi
            do_install
            ;;
        2)
            do_update
            ;;
        3)
            do_check_version
            ;;
        4)
            do_uninstall
            ;;
        0)
            echo -e "${green}再见！${plain}"
            exit 0
            ;;
        *)
            echo -e "${red}无效选择，请重新输入。${plain}"
            show_install_menu
            ;;
    esac
}

# --- 主安装流程 ---
do_install() {
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

    # 5. 配置面板（全新安装时含端口自定义交互；已有数据则保留原配置）
    configure_panel

    # 6. 获取面板端口（此时端口已最终确定）
    local panel_port=$(get_latest_version >/dev/null 2>&1; ${xui_folder}/x-ui setting -show true 2>/dev/null | grep 'port:' | awk -F: '{print $2}' | tr -d ' ')
    if [[ -z "${panel_port}" ]]; then
        panel_port=2053
    fi

    # 7. 配置防火墙（放行最终确定的面板端口）
    configure_firewall "${panel_port}"

    # 8. 显示完成信息
    echo -e "${green}安装完成！运行 x-ui 命令打开管理菜单。${plain}"
    echo -e "${yellow}提示: 如需 SSL 证书，请运行 x-ui 后选择 SSL 证书管理选项。${plain}"
}

# --- 入口 ---
if [[ $# -gt 0 ]]; then
    case "$1" in
        install|i)
            do_install "${2:-}"
            ;;
        update|u)
            do_update
            ;;
        version|v)
            do_check_version
            ;;
        uninstall|remove)
            do_uninstall
            ;;
        *)
            echo -e "${red}未知参数: $1${plain}"
            echo -e "用法: $0 [install|update|version|uninstall]"
            exit 1
            ;;
    esac
else
    show_install_menu
fi
