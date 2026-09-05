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
	// redir-host：global 也要 private 两个
	err := CheckRuleFiles(ModeGlobal, DNSModeRedirHost, dir)
	if err == nil {
		t.Fatal("空目录应报缺失")
	}
	if !strings.Contains(err.Error(), "geosite-private.mrs") || !strings.Contains(err.Error(), "geoip-private.mrs") {
		t.Fatalf("global 模式应需要 private 规则集: %v", err)
	}
	// fake-ip：额外要求 fakeipfilter
	err = CheckRuleFiles(ModeGlobal, DNSModeFakeIP, dir)
	if err == nil || !strings.Contains(err.Error(), "geosite-fakeipfilter") {
		t.Fatalf("fake-ip 模式应需要 geosite-fakeipfilter: %v", err)
	}
	// 补齐全部文件后两种 DNS 模式都通过
	for _, dnsMode := range []string{DNSModeRedirHost, DNSModeFakeIP} {
		for _, mode := range []string{ModeBypass, ModeBlacklist, ModeGlobal} {
			for _, tag := range builtinRuleFilesAll(mode, dnsMode) {
				os.MkdirAll(filepath.Join(dir, "srs"), 0755)
				os.MkdirAll(filepath.Join(dir, "mrs"), 0755)
				os.WriteFile(filepath.Join(dir, "srs", tag+".srs"), []byte("x"), 0644)
				os.WriteFile(filepath.Join(dir, "mrs", tag+".mrs"), []byte("x"), 0644)
			}
		}
	}
	for _, dnsMode := range []string{DNSModeRedirHost, DNSModeFakeIP} {
		for _, mode := range []string{ModeBypass, ModeBlacklist, ModeGlobal} {
			if err := CheckRuleFiles(mode, dnsMode, dir); err != nil {
				t.Fatalf("补齐后不应报错: %v", err)
			}
		}
	}
}

func TestBuildBuiltinSingBox(t *testing.T) {
	base := BuiltinOptions{
		TunStack: "gvisor", TunMTU: 9000, TunStrictRoute: true,
		ProxyEnabled: true, ProxyListen: "127.0.0.1", ProxyPort: 2080,
		RulesDir: "/run/rules", UIDir: "/run/ui",
		// Cfg 为 nil 时使用默认 BuiltinSettings
	}
	cases := []struct {
		mode      string
		wantFinal string
		wantDNS   string
		wantSets  int
	}{
		{ModeBypass, "proxy", "dns-proxy", 3},
		{ModeBlacklist, "direct", "dns-direct", 9},
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
		// default_domain_resolver 必须指向直连 DNS，且 IPv6 关闭时仅 IPv4
		dds, ok := route["default_domain_resolver"].(map[string]interface{})
		if !ok || dds["server"] != "dns-direct" || dds["strategy"] != "ipv4_only" {
			t.Errorf("[%s] default_domain_resolver 错误: %v", c.mode, route["default_domain_resolver"])
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
		} else if _, ok := route["rule_set"]; ok {
			t.Errorf("[%s] 不应生成 rule_set", c.mode)
		}
		// DNS：final + server 标签
		dns := cfg["dns"].(map[string]interface{})
		if dns["final"] != c.wantDNS {
			t.Errorf("[%s] dns.final = %v, want %s", c.mode, dns["final"], c.wantDNS)
		}
		// 1.12 新格式：每个 DNS server 必须带 type 字段；直连/代理带 domain_resolver
		for _, s := range dns["servers"].([]interface{}) {
			sm := s.(map[string]interface{})
			if sm["type"] == nil || sm["type"] == "" {
				t.Errorf("[%s] dns server %v 缺少 type 字段", c.mode, sm["tag"])
			}
			if sm["tag"] == "dns-direct" || sm["tag"] == "dns-proxy" {
				dr, ok := sm["domain_resolver"].(map[string]interface{})
				if !ok || dr["server"] != "dns-resolver" {
					t.Errorf("[%s] dns server %s 应含 domain_resolver→dns-resolver", c.mode, sm["tag"])
				}
			}
		}
		// outbounds: proxy + direct
		obs := cfg["outbounds"].([]interface{})
		if len(obs) != 2 {
			t.Errorf("[%s] outbounds 应为 [proxy direct], got %d 个", c.mode, len(obs))
		}
		// 日志等级（warning → sing-box warn）/ clash-api
		if lg := cfg["log"].(map[string]interface{}); lg["level"] != "warn" {
			t.Errorf("[%s] log.level = %v, want warn", c.mode, lg["level"])
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

// TestBuildBuiltinSingBoxFakeIP fake-ip 模式：fakeipfilter 白名单 → 直连 DNS，其余 A/AAAA → fakeip
func TestBuildBuiltinSingBoxFakeIP(t *testing.T) {
	opts := BuiltinOptions{
		Mode: ModeBypass, RulesDir: "/run/rules", UIDir: "/run/ui",
		Cfg: &BuiltinSettings{DNSMode: DNSModeFakeIP},
	}
	data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseJSONBytes(data)
	dns := cfg["dns"].(map[string]interface{})

	// servers 含 fakeip
	hasFakeIP := false
	for _, s := range dns["servers"].([]interface{}) {
		if s.(map[string]interface{})["type"] == "fakeip" {
			hasFakeIP = true
		}
	}
	if !hasFakeIP {
		t.Fatal("fake-ip 模式 servers 应含 fakeip server")
	}
	// rules：白名单 → dns-direct，其后 A/AAAA → dns-fakeip
	rules := dns["rules"].([]interface{})
	r0 := rules[0].(map[string]interface{})
	if !strings.Contains(fmtJoin(r0["rule_set"]), "geosite-fakeipfilter") || r0["server"] != "dns-direct" {
		t.Errorf("首条 DNS 规则应为 fakeipfilter→dns-direct, got %v", r0)
	}
	r1 := rules[1].(map[string]interface{})
	if r1["action"] != "route" || r1["server"] != "dns-fakeip" {
		t.Errorf("第二条 DNS 规则应为 route→dns-fakeip, got %v", r1)
	}
	// route 段 rule_set 应含 geosite-fakeipfilter
	foundFilter := false
	for _, rs := range cfg["route"].(map[string]interface{})["rule_set"].([]interface{}) {
		if rs.(map[string]interface{})["tag"] == "geosite-fakeipfilter" {
			foundFilter = true
		}
	}
	if !foundFilter {
		t.Fatal("fake-ip 模式 route.rule_set 应含 geosite-fakeipfilter")
	}
}

// TestBuildBuiltinSingBoxDNSFields DNS 服务器字段：地址必填；端口/路径选填
// （端口零值时按类型补默认端口，保证配置到手即用）
func TestBuildBuiltinSingBoxDNSFields(t *testing.T) {
	opts := BuiltinOptions{
		Mode: ModeGlobal, RulesDir: "/run/rules",
		Cfg: &BuiltinSettings{
			SingBoxDirect: DNSServer{Type: "https", Address: "dns.alidns.com", Path: "/dns-query"}, // 端口 0 → 补默认 443
			SingBoxProxy:  DNSServer{Type: "udp", Address: "8.8.8.8", Port: 53},
		},
	}
	data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseJSONBytes(data)
	dns := cfg["dns"].(map[string]interface{})
	for _, s := range dns["servers"].([]interface{}) {
		m := s.(map[string]interface{})
		switch m["tag"] {
		case "dns-direct":
			if sp, ok := m["server_port"].(float64); !ok || sp != 443 {
				t.Errorf("https 端口为 0 时应补默认 443: %v", m)
			}
			if m["path"] != "/dns-query" {
				t.Errorf("https 直连 DNS 应带 path: %v", m)
			}
		case "dns-proxy":
			if sp, ok := m["server_port"].(float64); !ok || sp != 53 {
				t.Errorf("代理 DNS 应带 server_port=53: %v", m)
			}
			if _, ok := m["path"]; ok {
				t.Errorf("非 https 类型不应写 path: %v", m)
			}
		}
	}
}

// TestBuildBuiltinSingBoxIPv6 IPv6 开关：TUN 地址补 IPv6 段 + 解析策略 prefer_ipv4
func TestBuildBuiltinSingBoxIPv6(t *testing.T) {
	opts := BuiltinOptions{
		Mode: ModeBypass, TunEnabled: true, RulesDir: "/run/rules",
		Cfg: &BuiltinSettings{DNSMode: DNSModeRedirHost, IPv6: true},
	}
	data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseJSONBytes(data)
	var tunAddr []interface{}
	for _, ib := range cfg["inbounds"].([]interface{}) {
		if m := ib.(map[string]interface{}); m["type"] == "tun" {
			tunAddr = m["address"].([]interface{})
		}
	}
	if len(tunAddr) != 2 || tunAddr[1] != "fdfe:dcba:9876::1/126" {
		t.Errorf("IPv6 开启时 TUN 地址应含 IPv6 段: %v", tunAddr)
	}
	dds := cfg["route"].(map[string]interface{})["default_domain_resolver"].(map[string]interface{})
	if dds["strategy"] != "prefer_ipv4" {
		t.Errorf("IPv6 开启时解析策略应为 prefer_ipv4: %v", dds)
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
		// proxies / proxy-groups / tun / mixed-port
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
		// 日志等级 / clash-api / ipv6
		if cfg["log-level"] != "warning" {
			t.Errorf("[%s] log-level = %v, want warning", c.mode, cfg["log-level"])
		}
		if cfg["external-controller"] != "127.0.0.1:9090" || cfg["secret"] != "" {
			t.Errorf("[%s] clash-api 配置错误: controller=%v secret=%v", c.mode, cfg["external-controller"], cfg["secret"])
		}
		if cfg["external-ui"] != "/run/ui" {
			t.Errorf("[%s] external-ui = %v, want /run/ui", c.mode, cfg["external-ui"])
		}
		if cfg["ipv6"] != false {
			t.Errorf("[%s] 默认 ipv6 应为 false", c.mode)
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
		if dns["nameserver"].([]interface{})[0] != proxy[0] {
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

// TestBuildBuiltinMihomoFakeIP fake-ip 模式：fake-ip-filter 引用 fakeipfilter 规则集
func TestBuildBuiltinMihomoFakeIP(t *testing.T) {
	opts := BuiltinOptions{
		Mode: ModeBypass, RulesDir: "/run/rules",
		Cfg: &BuiltinSettings{DNSMode: DNSModeFakeIP, IPv6: true},
	}
	data, err := BuildBuiltinConfig(CoreMihomo, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseYAMLBytes(data)
	dns := cfg["dns"].(map[string]interface{})
	if dns["enhanced-mode"] != "fake-ip" {
		t.Fatalf("enhanced-mode = %v, want fake-ip", dns["enhanced-mode"])
	}
	filter := toStringSlice(dns["fake-ip-filter"].([]interface{}))
	if len(filter) != 1 || filter[0] != "rule-set:geosite-fakeipfilter" {
		t.Fatalf("fake-ip-filter = %v, want [rule-set:geosite-fakeipfilter]", filter)
	}
	if dns["ipv6"] != true || cfg["ipv6"] != true {
		t.Fatal("IPv6 开启时顶层与 dns 的 ipv6 都应为 true")
	}
	// rule-providers 应含 geosite-fakeipfilter
	providers := cfg["rule-providers"].(map[string]interface{})
	if _, ok := providers["geosite-fakeipfilter"]; !ok {
		t.Fatal("fake-ip 模式 rule-providers 应含 geosite-fakeipfilter")
	}
}

// TestBuildBuiltinClashAPIDisabled 关闭 clash-api：双内核完全不生成 clash-api 字段
// （sing-box 的 cache_file 与 clash-api 无关，仍然生成）
func TestBuildBuiltinClashAPIDisabled(t *testing.T) {
	opts := BuiltinOptions{
		Mode: ModeBypass, RulesDir: "/run/rules", CachePath: "/run/cache.db",
		Cfg: &BuiltinSettings{DNSMode: DNSModeRedirHost, ClashAPIDisabled: true},
	}
	data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseJSONBytes(data)
	exp := cfg["experimental"].(map[string]interface{})
	if _, ok := exp["clash_api"]; ok {
		t.Error("clash-api 关闭时 sing-box 不应生成 clash_api 段")
	}
	cf := exp["cache_file"].(map[string]interface{})
	if cf["enabled"] != true || cf["store_fakeip"] != false {
		t.Errorf("cache_file 配置错误: %v", cf)
	}
	if cf["path"] != "/run/cache.db" {
		t.Errorf("cache_file.path = %v, want /run/cache.db", cf["path"])
	}
	data, err = BuildBuiltinConfig(CoreMihomo, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ = parseYAMLBytes(data)
	for _, k := range []string{"external-controller", "external-ui", "secret"} {
		if _, ok := cfg[k]; ok {
			t.Errorf("clash-api 关闭时 mihomo 不应生成 %s", k)
		}
	}
}

// TestBuildBuiltinSingBoxCacheFileFakeIP fake-ip 模式下 store_fakeip 应为 true
func TestBuildBuiltinSingBoxCacheFileFakeIP(t *testing.T) {
	opts := BuiltinOptions{
		Mode: ModeBypass, RulesDir: "/run/rules", CachePath: "/run/cache.db",
		Cfg: &BuiltinSettings{DNSMode: DNSModeFakeIP},
	}
	data, err := BuildBuiltinConfig(CoreSingBox, opts, testNode())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := parseJSONBytes(data)
	cf := cfg["experimental"].(map[string]interface{})["cache_file"].(map[string]interface{})
	if cf["store_fakeip"] != true {
		t.Errorf("fake-ip 模式 store_fakeip 应为 true: %v", cf)
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
	// builtin 段默认值
	if s.Builtin.LogLevel != "warning" || s.Builtin.DNSMode != "redir-host" || s.Builtin.ClashAPI.Port != 9090 {
		t.Fatalf("builtin 默认值错误: %+v", s.Builtin)
	}
	// 全新默认设置必须带完整 builtin 段（此前全新安装漏填，界面显示全 0）
	if d := Defaults(); d.Builtin.ClashAPI.Port != 9090 || d.Builtin.ResolverDNS == "" || d.Builtin.ResolverDNSBackup == "" {
		t.Fatalf("Defaults() 的 builtin 段缺省: %+v", d.Builtin)
	}
	// 解析 DNS 备用默认值
	if s.Builtin.ResolverDNSBackup != "119.29.29.29" {
		t.Fatalf("解析 DNS 备用默认值错误: %q", s.Builtin.ResolverDNSBackup)
	}
	// Validate 拒绝非法值
	s.RoutingMode = "bogus"
	if err := s.Validate(); err == nil {
		t.Fatal("非法路由模式应校验失败")
	}
	s = Settings{}
	s.applyDefaults()
	s.Builtin.DNSMode = "bogus"
	if err := s.Validate(); err == nil {
		t.Fatal("非法 DNS 模式应校验失败")
	}
	s = Settings{}
	s.applyDefaults()
	s.Builtin.ResolverDNS = "not-an-ip"
	if err := s.Validate(); err == nil {
		t.Fatal("解析 DNS 非 IP 应校验失败")
	}
	s = Settings{}
	s.applyDefaults()
	s.Builtin.ResolverDNSBackup = "not-an-ip"
	if err := s.Validate(); err == nil {
		t.Fatal("解析 DNS 备用非 IP 应校验失败")
	}
	s = Settings{}
	s.applyDefaults()
	s.Builtin.ClashAPI.Port = 0
	if err := s.Validate(); err == nil {
		t.Fatal("clash-api 端口为 0 应校验失败")
	}
}

// fmtJoin 把 rule_set 字段拼成字符串便于断言
func fmtJoin(v interface{}) string {
	switch l := v.(type) {
	case []interface{}:
		parts := make([]string, 0, len(l))
		for _, e := range l {
			parts = append(parts, e.(string))
		}
		return strings.Join(parts, ",")
	}
	return ""
}
