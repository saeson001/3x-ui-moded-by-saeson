# 3x-ui moded by saeson

> 基于 [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) 最新版的增强分支

## 新功能 (v1.2.0)

### Clash Link — 一键生成 Clash Party 配置文件

**功能：** 从 3x-ui 入站节点直接生成 Clash Meta / Clash Party 兼容的 YAML 配置文件链接。

- **VLESS+REALITY 完整兼容** — 正确输出 `public-key`、`short-id`、`client-fingerprint`、`servername` 等 REALITY 参数
- **多节点聚合** — 将多个入站合并到一个 Clash 配置
- **Token 订阅链接** — 生成固定 URL（`/d/<token>`），可直接粘贴到 Clash Party 作为远程订阅
- **配置备份导出** — 一键导出所有入站、客户端、Host 等设置到 JSON 文件
- **兼容原版数据库** — 直接读取 3x-ui 的 SQLite 数据库，无需额外配置

**使用方法：**

```bash
# API 方式生成（需先登录面板获取 session）
curl -X POST http://<面板地址>/panel/api/clash-link/generate \
  -H "Content-Type: application/json" \
  -d '{
    "inbound_ids": [],        # 空数组 = 全部启用的入站
    "config_name": "我的节点",
    "mixed_port": 7890,
    "allow_lan": true,
    "mode": "rule",
    "log_level": "info",
    "group_name": "节点选择"
  }'

# 返回：{"success":true, "full_url":"http://xxx/d/abcdef1234567890", "token":"...", "proxy_num":3}

# 备份导出
curl http://<面板地址>/panel/api/clash-link/backup > backup.json
```

**Clash Party 导入方法：**
1. 在 3x-ui 面板中生成配置链接
2. 复制返回的 `full_url`（如 `http://38.47.108.240:2053/d/abcdef1234567890`）
3. 打开 Clash Party → Profiles → 粘贴 URL → 下载
4. 切换到新 Profile 即可使用

## 修复内容

### External Proxy + REALITY Bug 修复 (v1.1.0 — Go 源码级)

**问题描述：** 当 VLESS+REALITY 入站启用了 External Proxy（dokodemo-door 中转）时，3x-ui 在生成 xray 配置 (`config.json`) 时会清空 REALITY 安全参数（`publicKey` → `null`，`fingerprint` → `null`），导致：
- v2rayN / Xray-core 客户端仍可连接（通过分享链接补充参数）
- **Clash Meta / Mihomo / Clash Party 客户端无法连接**（报错 "REALITY authentication failed"）

**根因：** 3x-ui 的 `AddInbound` 函数中，`database.CreateHostsFromExternalProxy()` 调用后 `stream_settings` 的 `realitySettings.settings.publicKey` 和 `realitySettings.settings.fingerprint` 被错误地覆盖为空值。

**修复 (v1.1.0)：** 
- 新增 `internal/web/service/reality_fix.go` — `preserveRealitySettings` 方法
- 在 `AddInbound` 的 `CreateHostsFromExternalProxy` 调用后，从数据库恢复被清空的 Reality 参数
- **编译修复版**: 通过 GitHub Actions 重新编译 x-ui 二进制，内置修复逻辑
- **降级方案**: 如果编译版暂未发布，自动使用运行时修复脚本 (`fix-reality-config.sh`)

### 2. 改进的安装脚本

- 全面中文本地化
- 多发行版支持：Debian / Ubuntu / CentOS / RHEL / Fedora / Arch / Alpine / openSUSE
- 支持 arm64 / amd64 / armv7 架构
- 保留原有全部功能（SSL 证书、Telegram Bot、节点管理等）
- 新增版本检查功能

## 一键安装

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/saeson001/3x-ui-moded-by-saeson/main/install.sh)
```

## 管理命令

安装完成后，随时在终端输入 `x-ui` 打开管理菜单：

```
x-ui                    # 打开管理菜单
x-ui version            # 查看版本
x-ui update             # 更新到最新版
x-ui uninstall          # 卸载
```

## 支持的协议

VLess / VMess / Trojan / ShadowSocks / WireGuard / Hysteria2 / TUN / HTTP / SOCKS / Dokodemo-door

## 支持的系统

| 发行版 | 架构 |
|--------|------|
| Debian 10+ | amd64, arm64, armv7 |
| Ubuntu 20.04+ | amd64, arm64, armv7 |
| CentOS 7/8/9 | amd64, arm64 |
| RHEL 8/9 | amd64, arm64 |
| AlmaLinux 8/9 | amd64, arm64 |
| Rocky Linux 8/9 | amd64, arm64 |
| Fedora 36+ | amd64, arm64 |
| Arch Linux | amd64, arm64 |
| Alpine 3.17+ | amd64, arm64 |
| openSUSE | amd64 |

## 原项目

本分支定期同步上游 [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) 的更新。

## 许可证

GPL-3.0
