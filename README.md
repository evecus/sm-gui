# SM GUI

一个 Windows 下的 sing-box / mihomo 图形化管理工具，类似 v2rayN。

## 功能

- 节点列表管理（导入、订阅、右键应用节点）
- 支持协议：VMess / VLESS / Trojan / Shadowsocks / Hysteria / Hysteria2 / TUIC / AnyTLS / SSR / WireGuard 等
- 订阅格式：base64 节点链接 / Clash YAML / sing-box JSON
- 双内核：设置中可切换 sing-box（json 配置）/ mihomo（yaml 配置）
- 内置路由配置（参照 v2rayN）：**绕过大陆 / GFW列表 / 全局代理**，模板合成、无需手写配置文件
- 一键启用 TUN 模式
- 一键设置系统代理
- 启动/停止内核进程 + 实时日志

## 使用方法

1. 解压后目录结构：
   ```
   sm-gui/
   ├── sm-gui.exe       ← 主程序
   ├── bin/
   │   ├── sing-box.exe ← sing-box 内核（可选）
   │   └── mihomo.exe   ← mihomo 内核（可选）
   ├── configs/         ← 放置配置文件（sing-box 用 json，mihomo 用 yaml）
   ├── run/
   │   └── rules/       ← 内置路由规则集（srs/ + mrs/，Release 包已内置）
   ├── run/             ← 自动生成（内核运行目录）
   ├── data/            ← 自动生成（节点/设置持久化）
   └── config.example.json
   ```

2. 准备内核配置文件放入 `configs/` 目录：
   - sing-box：json 配置，outbounds 里需有一个 `"tag": "proxy"` 的出站（参考 `config.example.json`）
   - mihomo：yaml 配置，proxies 里会有一个 `name: proxy` 的条目由程序写入（无则自动创建）

3. 启动 `sm-gui.exe`，在设置中选择内核，点击顶部配置栏选择你的配置文件。

4. 导入节点或拉取订阅，右键节点 → **应用此节点**，
   程序会将该节点写入配置文件（sing-box 写 `proxy` outbound，mihomo 写 `proxy` proxies 条目）。

5. 按需开启底部三个开关：
   - **TUN 模式**：向配置文件写入 TUN 配置（需要管理员权限）
   - **系统代理**：设置 Windows 系统代理为 `127.0.0.1:2080`
   - **启动核心**：运行 `bin/` 下当前内核的程序

## 注意

- TUN 模式需要以**管理员身份**运行程序
- 系统代理自动添加常见内网地址到绕过列表
- 程序退出时会自动停止内核进程

## 内置路由配置

配置文件下拉末尾有三个内置项（模板合成，**不修改 configs 目录中的文件**，直接生成到 `run/`）：

| 内置项 | 语义（对齐 v2rayN） |
|---|---|
| 内置配置：绕过大陆 | 国内域名/IP、私网、国内公共 DNS 直连，其余走代理 |
| 内置配置：GFW列表 | 被墙域名（gfw/greatfire）及海外服务 IP 走代理，其余直连 |
| 内置配置：全局代理 | 仅私网直连，全部走代理 |

使用前置条件：

1. 先在节点列表**应用一个节点**（内置配置的 `proxy` 出站来自该节点）
2. 规则文件放入 `run/rules/`（**Release 包已内置**，自己编译时从仓库 `ruleset/` 目录复制）：
   - sing-box：`run/rules/srs/<tag>.srs`，来源 [SagerNet/sing-geosite](https://github.com/SagerNet/sing-geosite/releases)、[SagerNet/sing-geoip](https://github.com/SagerNet/sing-geoip/releases)
   - mihomo：`run/rules/mrs/<tag>.mrs`（behavior：geosite→domain，geoip→ipcidr），来源 [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat)
   - 需要的 tag：绕过大陆 = `geosite-private` `geoip-private` `geosite-cn` `geosite-google` `geoip-cn`；GFW列表 = `geosite-private` `geoip-private` `geosite-google` `geosite-gfw` `geosite-greatfire` `geoip-facebook` `geoip-fastly` `geoip-google` `geoip-netflix` `geoip-telegram` `geoip-twitter`；全局代理（mihomo）= `geosite-private` `geoip-private`（sing-box 全局用原生 `ip_is_private`，无需规则文件）
3. sing-box 内核要求 **≥ 1.12**（rule action / dns rule_set 语法）

内置配置同时启用 **clash-api**（`127.0.0.1:9090`，无密码），内核启动后浏览器打开 `http://127.0.0.1:9090/ui` 可访问管理面板（面板文件位于 `run/ui`，sing-box 首次启动会自动下载默认面板）。

## 从源码编译

```bash
# 安装依赖
go install github.com/wailsapp/wails/v2/cmd/wails@latest
cd frontend && npm install && cd ..

# 开发模式
wails dev

# 编译
wails build -platform windows/amd64 -ldflags "-H windowsgui"
```

## GitHub Actions 自动编译

推送 tag 即可触发自动编译并发布 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```
