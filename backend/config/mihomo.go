package config

import (
	"fmt"
	"os"

	"sm-gui/backend/node"

	"gopkg.in/yaml.v3"
)

// mihomo（Clash.Meta）配置采用与 sing-box 不同的约定：
//
//   - proxies 中 name 为 "proxy" 的条目代表"当前应用的节点"（对齐 sing-box 的 tag:proxy）；
//   - proxy-groups 中有一个名为 "PROXY" 的 select 组引用它（组名大写，避免与节点同名冲突）；
//   - TUN 是顶层 tun 对象（enable/stack/mtu/auto-route...），mihomo 没有 strict_route；
//   - 系统代理对应顶层 mixed-port + allow-lan。

const (
	mihomoProxyName = "proxy"
	mihomoGroupName = "PROXY"
)

// ─── YAML load / save ─────────────────────────────────────────────────────────

func loadYAML(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return cfg, nil
}

func saveYAML(path string, cfg map[string]interface{}) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	// atomic write: temp file + rename to avoid corrupting the config on crash
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// getProxies 返回配置中的 proxies 列表。
func getProxies(cfg map[string]interface{}) []interface{} {
	v, _ := cfg["proxies"].([]interface{})
	return v
}

// cloneMap 深拷贝一个 map（经 yaml 序列化往返，同时把类型规范化为 yaml.v3 的原生类型，
// 保证写入与回读后逐字段可比较）。
func cloneMap(m map[string]interface{}) map[string]interface{} {
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// ─── Apply Node ───────────────────────────────────────────────────────────────

// ApplyNodeToMihomoConfig 把节点写入 mihomo 配置：
// 替换/追加 name 为 "proxy" 的 proxies 条目，并确保 PROXY 选择组引用它。
func ApplyNodeToMihomoConfig(cfgPath string, n node.Node) error {
	cfg, err := loadYAML(cfgPath)
	if err != nil {
		return err
	}
	var proxy map[string]interface{}
	if n.RawClashProxy != nil {
		// 原始 Clash YAML 条目无损回写（同 sing-box RawOutbound 的做法）
		proxy = cloneMap(n.RawClashProxy)
		if proxy == nil {
			return fmt.Errorf("节点原始 Clash 数据无效")
		}
	} else {
		proxy, err = node.NodeToClashProxy(n)
		if err != nil {
			return err
		}
	}
	proxy["name"] = mihomoProxyName

	proxies := getProxies(cfg)
	replaced := false
	for i, pr := range proxies {
		if m, ok := pr.(map[string]interface{}); ok && m["name"] == mihomoProxyName {
			proxies[i] = proxy
			replaced = true
			break
		}
	}
	if !replaced {
		proxies = append(proxies, proxy)
	}
	cfg["proxies"] = proxies
	ensureMihomoProxyGroup(cfg)
	return saveYAML(cfgPath, cfg)
}

// ensureMihomoProxyGroup 确保 proxy-groups 中存在引用 "proxy" 条目的
// PROXY select 组；不存在则创建，存在则把 "proxy" 补进其成员列表。
func ensureMihomoProxyGroup(cfg map[string]interface{}) {
	groups, _ := cfg["proxy-groups"].([]interface{})
	for _, g := range groups {
		m, ok := g.(map[string]interface{})
		if !ok || m["name"] != mihomoGroupName {
			continue
		}
		members, _ := m["proxies"].([]interface{})
		for _, mem := range members {
			if s, ok := mem.(string); ok && s == mihomoProxyName {
				return // 已引用，无需修改
			}
		}
		m["proxies"] = append(members, mihomoProxyName)
		return
	}
	cfg["proxy-groups"] = append(groups, map[string]interface{}{
		"name":    mihomoGroupName,
		"type":    "select",
		"proxies": []interface{}{mihomoProxyName, "DIRECT"},
	})
}

// ─── TUN ──────────────────────────────────────────────────────────────────────

// SetTunMihomo 写/删顶层 tun 配置。mihomo 没有 strict_route，调用方已忽略该开关。
// 启用时保留用户 tun 块中的其他字段，仅覆盖本 GUI 管理的字段。
func SetTunMihomo(cfgPath string, enable bool, stack string, mtu int) error {
	cfg, err := loadYAML(cfgPath)
	if err != nil {
		return err
	}
	if !enable {
		delete(cfg, "tun")
		return saveYAML(cfgPath, cfg)
	}
	tun, _ := cfg["tun"].(map[string]interface{})
	if tun == nil {
		tun = map[string]interface{}{}
	}
	if stack != "gvisor" && stack != "system" && stack != "mixed" {
		stack = "gvisor"
	}
	if mtu <= 0 {
		mtu = 9000
	}
	tun["enable"] = true
	tun["stack"] = stack
	tun["mtu"] = mtu
	tun["auto-route"] = true
	tun["auto-detect-interface"] = true
	if _, ok := tun["dns-hijack"]; !ok {
		tun["dns-hijack"] = []interface{}{"any:53"}
	}
	cfg["tun"] = tun
	return saveYAML(cfgPath, cfg)
}

// HasTunMihomo 判断配置是否启用了 tun（tun.enable == true）。
func HasTunMihomo(cfgPath string) bool {
	cfg, err := loadYAML(cfgPath)
	if err != nil {
		return false
	}
	tun, ok := cfg["tun"].(map[string]interface{})
	if !ok {
		return false
	}
	enable, _ := tun["enable"].(bool)
	return enable
}

// ─── 系统代理（mixed-port）─────────────────────────────────────────────────────

// SetMixedInboundMihomo 写/删顶层 mixed-port。
// mihomo 没有逐 inbound 的监听地址：127.0.0.1 → 仅本机（allow-lan: false），
// 0.0.0.0 / :: → 允许局域网（allow-lan: true + bind-address: "*"）。
func SetMixedInboundMihomo(cfgPath string, enable bool, listen string, port int) error {
	cfg, err := loadYAML(cfgPath)
	if err != nil {
		return err
	}
	if !enable {
		delete(cfg, "mixed-port")
		return saveYAML(cfgPath, cfg)
	}
	if port <= 0 {
		port = 2080
	}
	cfg["mixed-port"] = port
	allowLan := listen != "127.0.0.1"
	cfg["allow-lan"] = allowLan
	if allowLan {
		cfg["bind-address"] = "*"
	}
	return saveYAML(cfgPath, cfg)
}

// ─── Applied node detection ──────────────────────────────────────────────────

// FindAppliedNodeIDMihomo 找出配置中 name 为 "proxy" 的条目对应的节点 ID。
// 比较方式：把该条目与每个节点的（RawClashProxy 或生成的 Clash 条目）
// 规范化为 YAML 后逐一比对。找不到匹配返回 ""。
func FindAppliedNodeIDMihomo(cfgPath string, nodes []node.Node) string {
	cfg, err := loadYAML(cfgPath)
	if err != nil {
		return ""
	}
	var current map[string]interface{}
	for _, pr := range getProxies(cfg) {
		if m, ok := pr.(map[string]interface{}); ok && m["name"] == mihomoProxyName {
			current = cloneMap(m)
			break
		}
	}
	if current == nil {
		return ""
	}
	curData, err := yaml.Marshal(current)
	if err != nil {
		return ""
	}
	for _, n := range nodes {
		var p map[string]interface{}
		if n.RawClashProxy != nil {
			p = cloneMap(n.RawClashProxy)
		} else {
			var err error
			p, err = node.NodeToClashProxy(n)
			if err != nil {
				continue
			}
		}
		if p == nil {
			continue
		}
		p["name"] = mihomoProxyName
		data, err := yaml.Marshal(p)
		if err != nil {
			continue
		}
		if string(data) == string(curData) {
			return n.ID
		}
	}
	return ""
}
