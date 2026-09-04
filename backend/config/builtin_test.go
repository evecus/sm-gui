package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sm-gui/backend/node"

	"gopkg.in/yaml.v3"
)

// 测试辅助：解析生成的配置
func parseJSONBytes(data []byte) (map[string]interface{}, error) {
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseYAMLBytes(data []byte) (map[string]interface{}, error) {
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func toStringSlice(v []interface{}) []string {
	out := make([]string, 0, len(v))
	for _, e := range v {
		out = append(out, e.(string))
	}
	return out
}

// assertMihomoDNS 校验各模式 mihomo DNS：redir-host + nameserver-policy 分流策略。
func assertMihomoDNS(t *testing.T, mode string, dnsV interface{}) {
	t.Helper()
	dns, ok := dnsV.(map[string]interface{})
	if !ok {
		t.Fatalf("[%s] dns 段缺失", mode)
	}
	if dns["enable"] != true || dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("[%s] dns 应为启用的 redir-host 模式", mode)
	}
	direct := []interface{}{"223.5.5.5", "119.29.29.29"}
	proxy := []interface{}{"1.1.1.1#PROXY", "8.8.8.8#PROXY"}
	policy, hasPolicy := dns["nameserver-policy"].(map[string]interface{})
	switch mode {
	case ModeBypass:
		if len(toStringSlice(dns["nameserver"].([]interface{}))) == 0 ||
			dns["nameserver"].([]interface{})[0] != proxy[0] {
			t.Errorf("[%s] nameserver 应为代理 DNS, got %v", mode, dns["nameserver"])
		}
		if !hasPolicy || len(policy["rule-set:geosite-cn"].([]interface{})) != 2 {
			t.Errorf("[%s] nameserver-policy 应含 rule-set:geosite-cn → 直连 DNS", mode)
		}
	case ModeBlacklist:
		if dns["nameserver"].([]interface{})[0] != direct[0] {
			t.Errorf("[%s] nameserver 应为直连 DNS", mode)
		}
		if !hasPolicy || len(policy["rule-set:geosite-gfw"].([]interface{})) != 2 {
			t.Errorf("[%s] nameserver-policy 应含 rule-set:geosite-gfw → 代理 DNS", mode)
		}
		if policy != nil && policy["rule-set:geosite-cn"] != nil {
			t.Errorf("[%s] blacklist 不应把 cn 规则集写进 DNS 策略", mode)
		}
	case ModeGlobal:
		// 全部走代理：默认 nameserver 也是代理 DNS
		if dns["nameserver"].([]interface{})[0] != proxy[0] {
			t.Errorf("[%s] nameserver 应为代理 DNS", mode)
		}
		if hasPolicy {
			t.Errorf("[%s] global 不应有 nameserver-policy", mode)
		}
	}
}

// 测试用节点（vless，带 RawOutbound / RawClashProxy 两条路径都覆盖）
func testNode() *node.Node {
	return &node.Node{
		ID:       "n1",
		Protocol: "vless",
		Address:  "example.com",
		Port:     443,
		VLESS: &node.VLESSConfig{
			UUID: "b831381d-6324-4d53-ad4f-8cda48b30811",
			TLS:  true,
			SNI:  "example.com",
		},
	}
}

func TestBuiltinNameRoundTrip(t *testing.T) {
	for _, mode := range []string{ModeBypass, ModeBlacklist, ModeGlobal} {
		name, ok := BuiltinDisplayName(mode)
		if !ok || !strings.HasPrefix(name, BuiltinPrefix) {
			t.Fatalf("BuiltinDisplayName(%s) = %q, %v", mode, name, ok)
		}
		got, ok := ParseBuiltinName(name)
		if !ok || got != mode {
			t.Fatalf("ParseBuiltinName(%q) = %q, %v; want %s", name, got, ok, mode)
		}
	}
	if _, ok := ParseBuiltinName("config.json"); ok {
		t.Fatal("真实文件名不应被解析为内置模式")
	}
	if IsBuiltinMode(ModeCustom) {
		t.Fatal("custom 不应被视为内置模式")
	}
}

func TestCheckRuleFiles(t *testing.T) {
	dir := t.TempDir()
	// 三种模式都需要规则文件（global 也要 private 两个）
	err := CheckRuleFiles(ModeGlobal, dir)
	if err == nil {
		t.Fatal("空目录应报缺失")
	}
	if !strings.Contains(err.Error(), "geosite-private.mrs") || !strings.Contains(err.Error(), "geoip-private.mrs") {
		t.Fatalf("global 模式应需要 private 规则集: %v", err)
	}
	// bypass 缺文件 → 报错并列出缺失项
	err = CheckRuleFiles(ModeBypass, dir)
	if err == nil {
		t.Fatal("空目录应报缺失")
	}
	for _, want := range []string{"geosite-cn.srs", "geosite-cn.mrs", "geoip-cn.srs"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息应包含 %s: %v", want, err)
		}
	}
	// 补齐文件后通过
	for _, mode := range []string{ModeBypass, ModeBlacklist, ModeGlobal} {
		for _, tag := range builtinRuleFilesAll(mode) {
			os.MkdirAll(filepath.Join(dir, "srs"), 0755)
			os.MkdirAll(filepath.Join(dir, "mrs"), 0755)
			os.WriteFile(filepath.Join(dir, "srs", tag+".srs"), []byte("x"), 0644)
			os.WriteFile(filepath.Join(dir, "mrs", tag+".mrs"), []byte("x"), 0644)
		}
	}
	for _, mode := range []string{ModeBypass, ModeBlacklist, ModeGlobal} {
		if err := CheckRuleFiles(mode, dir); err != nil {
			t.Fatalf("补齐后不应报错: %v", err)
		}
	}
}

func TestBuildBuiltinSingBox(t *testing.T) {
	base := BuiltinOptions{
		TunStack: "gvisor", TunMTU: 9000, TunStrictRoute: true,
		ProxyEnabled: true, ProxyListen: "127.0.0.1", ProxyPort: 2080,
		RulesDir: "/run/rules", UIDir: "/run/ui",
	}
	cases := []struct {
		mode      string
		wantFinal string
		wantDNS   string
		wantSets  int
	}{
		{ModeBypass, "proxy", "dns-proxy", 3},
		{ModeBlacklist, "direct", "dns-local", 9},
		{ModeGlobal, "proxy", "dns-proxy", 0},
	}
	for _, c := range cases {
		opts := base
		opts.Mode = c.mode
		opts.TunEnabled = true
		data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
		if err != nil {
			t.Fatalf("[%s] 生成失败: %v", c.mode, err)
		}
		cfg, err := parseJSONBytes(data)
		if err != nil {
			t.Fatalf("[%s] JSON 解析失败: %v", c.mode, err)
		}
		route := cfg["route"].(map[string]interface{})
		if route["final"] != c.wantFinal {
			t.Errorf("[%s] route.final = %v, want %s", c.mode, route["final"], c.wantFinal)
		}
		// default_domain_resolver 必须指向直连 DNS
		dds, ok := route["default_domain_resolver"].(map[string]interface{})
		if !ok || dds["server"] != "dns-local" {
			t.Errorf("[%s] route.default_domain_resolver 应为 {server: dns-local}, got %v", c.mode, route["default_domain_resolver"])
		}
		// sniff 必须是首条规则
		rules := route["rules"].([]interface{})
		first := rules[0].(map[string]interface{})
		if first["action"] != "sniff" {
			t.Errorf("[%s] 首条规则应为 sniff, got %v", c.mode, first)
		}
		// TUN 开启 → 含 hijack-dns + auto_detect_interface
		foundHijack := false
		for _, r := range rules {
			if m, ok := r.(map[string]interface{}); ok && m["action"] == "hijack-dns" {
				foundHijack = true
			}
		}
		if !foundHijack {
			t.Errorf("[%s] TUN 开启时应含 hijack-dns 规则", c.mode)
		}
		if route["auto_detect_interface"] != true {
			t.Errorf("[%s] TUN 开启时应含 auto_detect_interface", c.mode)
		}
		// rule_set 数量与引用路径
		if c.wantSets > 0 {
			rs := route["rule_set"].([]interface{})
			if len(rs) != c.wantSets {
				t.Errorf("[%s] rule_set 数量 = %d, want %d", c.mode, len(rs), c.wantSets)
			}
			for _, e := range rs {
				m := e.(map[string]interface{})
				if m["type"] != "local" || m["format"] != "binary" {
					t.Errorf("[%s] rule_set %v 应为 local/binary", c.mode, m["tag"])
				}
				if !strings.HasSuffix(m["path"].(string), ".srs") {
					t.Errorf("[%s] rule_set path 应指向 .srs: %v", c.mode, m["path"])
				}
			}
		} else if _, ok := route["rule_set"]; ok {
			t.Errorf("[%s] 不应生成 rule_set", c.mode)
		}
		// DNS
		dns := cfg["dns"].(map[string]interface{})
		if dns["final"] != c.wantDNS {
			t.Errorf("[%s] dns.final = %v, want %s", c.mode, dns["final"], c.wantDNS)
		}
		// 1.12 新格式：每个 DNS server 必须带 type 字段，否则内核报错
		for _, s := range dns["servers"].([]interface{}) {
			sm := s.(map[string]interface{})
			if sm["type"] == nil || sm["type"] == "" {
				t.Errorf("[%s] dns server %v 缺少 type 字段", c.mode, sm["tag"])
			}
		}
		// outbounds: proxy + direct
		obs := cfg["outbounds"].([]interface{})
		if len(obs) != 2 {
			t.Errorf("[%s] outbounds 应为 [proxy direct], got %d 个", c.mode, len(obs))
		}
		// 日志等级 / clash-api
		if lg := cfg["log"].(map[string]interface{}); lg["level"] != "warning" {
			t.Errorf("[%s] log.level = %v, want warning", c.mode, lg["level"])
		}
		api := cfg["experimental"].(map[string]interface{})["clash_api"].(map[string]interface{})
		if api["external_controller"] != "127.0.0.1:9090" || api["secret"] != "" {
			t.Errorf("[%s] clash_api 配置错误: %v", c.mode, api)
		}
		if api["external_ui"] != "/run/ui" {
			t.Errorf("[%s] clash_api.external_ui = %v, want /run/ui", c.mode, api["external_ui"])
		}
		// inbounds: mixed + tun
		ibs := cfg["inbounds"].([]interface{})
		kinds := map[string]bool{}
		for _, ib := range ibs {
			kinds[ib.(map[string]interface{})["type"].(string)] = true
		}
		if !kinds["mixed"] || !kinds["tun"] {
			t.Errorf("[%s] inbounds 应含 mixed+tun, got %v", c.mode, kinds)
		}
	}
}

func TestBuildBuiltinSingBoxInboundSwitches(t *testing.T) {
	opts := BuiltinOptions{Mode: ModeGlobal, RulesDir: "/run/rules"}
	data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseJSONBytes(data)
	if ibs := cfg["inbounds"].([]interface{}); len(ibs) != 0 {
		t.Errorf("TUN/代理全关时 inbounds 应为空, got %v", ibs)
	}
	if _, ok := cfg["route"].(map[string]interface{})["auto_detect_interface"]; ok {
		t.Error("TUN 关闭时不应有 auto_detect_interface")
	}
}

func TestBuildBuiltinMihomo(t *testing.T) {
	base := BuiltinOptions{
		TunStack: "gvisor", TunMTU: 9000,
		ProxyEnabled: true, ProxyListen: "127.0.0.1", ProxyPort: 2080,
		RulesDir: "/run/rules", UIDir: "/run/ui",
	}
	cases := []struct {
		mode       string
		wantMatch  string
		wantSets   int
		wantBypass bool // 绕过大陆应有 geosite-cn 直连
	}{
		{ModeBypass, "MATCH,PROXY", 5, true},
		{ModeBlacklist, "MATCH,DIRECT", 11, false},
		{ModeGlobal, "MATCH,PROXY", 2, false},
	}
	for _, c := range cases {
		opts := base
		opts.Mode = c.mode
		opts.TunEnabled = true
		data, err := BuildBuiltinConfig(CoreMihomo, opts, testNode())
		if err != nil {
			t.Fatalf("[%s] 生成失败: %v", c.mode, err)
		}
		cfg, err := parseYAMLBytes(data)
		if err != nil {
			t.Fatalf("[%s] YAML 解析失败: %v", c.mode, err)
		}
		// rules 末条 MATCH
		rules := toStringSlice(cfg["rules"].([]interface{}))
		if rules[len(rules)-1] != c.wantMatch {
			t.Errorf("[%s] 末条规则 = %s, want %s", c.mode, rules[len(rules)-1], c.wantMatch)
		}
		// 私网直连走 private 规则集（不再内联 CIDR）
		rulesSet := map[string]bool{}
		for _, r := range rules {
			rulesSet[r] = true
		}
		if !rulesSet["RULE-SET,geosite-private,DIRECT"] {
			t.Errorf("[%s] 应含 RULE-SET,geosite-private,DIRECT", c.mode)
		}
		if !rulesSet["RULE-SET,geoip-private,DIRECT,no-resolve"] {
			t.Errorf("[%s] 应含 RULE-SET,geoip-private,DIRECT,no-resolve", c.mode)
		}
		// no-resolve 策略：仅 geoip-cn 不加
		if c.wantBypass && !rulesSet["RULE-SET,geoip-cn,DIRECT"] {
			t.Errorf("[%s] geoip-cn 规则应不带 no-resolve", c.mode)
		}
		// rule-providers
		if c.wantSets > 0 {
			providers := cfg["rule-providers"].(map[string]interface{})
			if len(providers) != c.wantSets {
				t.Errorf("[%s] rule-providers 数量 = %d, want %d", c.mode, len(providers), c.wantSets)
			}
			for name, p := range providers {
				pm := p.(map[string]interface{})
				if pm["type"] != "file" || pm["format"] != "mrs" {
					t.Errorf("[%s] provider %s 应为 file/mrs", c.mode, name)
				}
				wantBehavior := "ipcidr"
				if isGeositeTag(name) {
					wantBehavior = "domain"
				}
				if pm["behavior"] != wantBehavior {
					t.Errorf("[%s] provider %s behavior = %v, want %s", c.mode, name, pm["behavior"], wantBehavior)
				}
				if !strings.HasSuffix(pm["path"].(string), ".mrs") {
					t.Errorf("[%s] provider %s path 应指向 .mrs", c.mode, name)
				}
			}
		} else if _, ok := cfg["rule-providers"]; ok {
			t.Errorf("[%s] 不应生成 rule-providers", c.mode)
		}
		// 绕过大陆：geosite-cn 直连存在且在 MATCH 之前
		if c.wantBypass {
			idxCN, idxMatch := -1, -1
			for i, r := range rules {
				if r == "RULE-SET,geosite-cn,DIRECT" {
					idxCN = i
				}
				if r == c.wantMatch {
					idxMatch = i
				}
			}
			if idxCN < 0 || idxCN > idxMatch {
				t.Errorf("[%s] geosite-cn 直连规则缺失或顺序错误", c.mode)
			}
		}
		// proxies / proxy-groups / dns / tun / mixed-port
		proxies := cfg["proxies"].([]interface{})
		if len(proxies) != 1 || proxies[0].(map[string]interface{})["name"] != mihomoProxyName {
			t.Errorf("[%s] proxies 应只含 name=proxy 条目", c.mode)
		}
		if cfg["mixed-port"] != 2080 {
			t.Errorf("[%s] mixed-port = %v, want 2080", c.mode, cfg["mixed-port"])
		}
		tun := cfg["tun"].(map[string]interface{})
		if tun["enable"] != true || tun["auto-route"] != true {
			t.Errorf("[%s] tun 配置不完整: %v", c.mode, tun)
		}
		// 日志等级 / clash-api
		if cfg["log-level"] != "warning" {
			t.Errorf("[%s] log-level = %v, want warning", c.mode, cfg["log-level"])
		}
		if cfg["external-controller"] != "127.0.0.1:9090" || cfg["secret"] != "" {
			t.Errorf("[%s] clash-api 配置错误: controller=%v secret=%v", c.mode, cfg["external-controller"], cfg["secret"])
		}
		if cfg["external-ui"] != "/run/ui" {
			t.Errorf("[%s] external-ui = %v, want /run/ui", c.mode, cfg["external-ui"])
		}
		// sniffer（三种模式固定）
		sn, ok := cfg["sniffer"].(map[string]interface{})
		if !ok || sn["enable"] != true {
			t.Errorf("[%s] sniffer 应存在且启用", c.mode)
		}
		// DNS：redir-host + nameserver-policy 分流
		assertMihomoDNS(t, c.mode, cfg["dns"])
	}
}

func TestBuildBuiltinErrors(t *testing.T) {
	opts := BuiltinOptions{Mode: ModeBypass, RulesDir: "/run/rules"}
	// 无节点
	if _, err := BuildBuiltinConfig(CoreSingBox, opts, nil); err == nil {
		t.Fatal("无节点应报错")
	}
	// 非内置模式
	if _, err := BuildBuiltinConfig(CoreSingBox, BuiltinOptions{Mode: ModeCustom}, testNode()); err == nil {
		t.Fatal("custom 模式不应生成内置配置")
	}
}

func TestSettingsRoutingModeDefaults(t *testing.T) {
	s := Settings{}
	s.applyDefaults()
	if s.RoutingMode != ModeCustom {
		t.Fatalf("缺省路由模式应为 custom, got %q", s.RoutingMode)
	}
	// Validate 拒绝非法值
	s.RoutingMode = "bogus"
	if err := s.Validate(); err == nil {
		t.Fatal("非法路由模式应校验失败")
	}
}
