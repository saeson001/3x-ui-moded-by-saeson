# 3x-ui moded by saeson

> 基于 [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) 最新版的增强分支

## 修复内容

### 1. External Proxy + REALITY Bug 修复

**问题描述：** 当 VLESS+REALITY 入站启用了 External Proxy（dokodemo-door 中转）时，3x-ui 在生成 xray 配置 (`config.json`) 时会清空 REALITY 安全参数（`publicKey` → `null`，`fingerprint` → `null`），导致：
- v2rayN / Xray-core 客户端仍可连接（通过分享链接补充参数）
- **Clash Meta / Mihomo / Clash Party 客户端无法连接**（报错 "REALITY authentication failed"）

**根因：** 3x-ui 的 `internal/web/service/inbound.go` 在调用 `database.CreateHostsFromExternalProxy()` 后，`stream_settings` 的 `realitySettings.settings.publicKey` 和 `realitySettings.settings.fingerprint` 被错误地覆盖为空值。

**修复：** 在生成 xray 配置时，确保从数据库读取的完整 `realitySettings` 被原样保留，不被 External Proxy 配置覆盖。

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
