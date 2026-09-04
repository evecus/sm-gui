package config

// 内置路由模式：参照 v2rayN 的三种路由模板（ServiceLib/Sample/custom_routing_*）生成
// sing-box / mihomo 配置，直接合成到 run 目录，不修改用户 configs/ 中的配置文件。
//
//   - bypass    绕过大陆（v2rayN "Whitelist"）：国内/私网直连，其余走代理，final=proxy
//   - blacklist GFW列表（v2rayN "Blacklist"）：被墙域名/海外服务 IP 走代理，其余直连，final=direct
//   - global    全局代理（v2rayN "Global"）：仅私网直连，其余走代理，final=proxy
//
// 可配置参数来自 Settings.Builtin（日志等级、DNS 模式、DNS 服务器、clash-api、IPv6），
// 默认值与此前的写死值一致。
//
// geosite/geoip 规则引用本地规则文件：
//   - sing-box: run/rules/srs/<tag>.srs  （local rule_set, format=binary）
//   - mihomo:   run/rules/mrs/<tag>.mrs  （file rule-provider, format=mrs）
// 私网直连：sing-box 用原生 ip_is_private（零文件依赖）；
// mihomo 用 geosite-private / geoip-private 规则集（private.mrs）。
// no-resolve 策略：仅 geoip-cn 不加（域名需解析为 IP 命中国内 IP 段），其余 IP 规则集都加。
// fake-ip 模式：geosite-fakeipfilter 规则集内的域名走直连 DNS（真实解析），其余都走 fakeip。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sm-gui/backend/node"

	"gopkg.in/yaml.v3"
)

// marshalJSON / marshalYAML 内置配置序列化。
func marshalJSON(cfg map[string]interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成 JSON 配置失败: %v", err)
	}
	return data, nil
}

func marshalYAML(cfg map[string]interface{}) ([]byte, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("生成 YAML 配置失败: %v", err)
	}
	return data, nil
}

// 路由模式常量（与 Settings.RoutingMode 对应）。
const (
	ModeCustom    = "custom"
	ModeBypass    = "bypass"
	ModeBlacklist = "blacklist"
	ModeGlobal    = "global"
)

// DNS 模式常量（与 Settings.Builtin.DNSMode 对应）。
const (
	DNSModeRedirHost = "redir-host"
	DNSModeFakeIP    = "fake-ip"
)

// BuiltinPrefix 内置配置在下拉列表中的显示前缀。
const BuiltinPrefix = "内置配置："

// builtinModeNames 内置模式 → 显示名（顺序即下拉顺序）。
var builtinModeNames = []struct {
	Mode string
	Name string
}{
	{ModeBypass, "绕过大陆"},
	{ModeBlacklist, "GFW列表"},
	{ModeGlobal, "全局代理"},
}

// IsBuiltinMode 判断是否为内置路由模式。
func IsBuiltinMode(mode string) bool {
	return mode == ModeBypass || mode == ModeBlacklist || mode == ModeGlobal
}

// BuiltinDisplayName 返回内置模式的下拉显示名（如 "内置配置：绕过大陆"）。
func BuiltinDisplayName(mode string) (string, bool) {
	for _, m := range builtinModeNames {
		if m.Mode == mode {
			return BuiltinPrefix + m.Name, true
		}
	}
	return "", false
}

// ParseBuiltinName 把下拉项解析为路由模式（仅接受 "内置配置：xxx" 形式）。
func ParseBuiltinName(name string) (string, bool) {
	for _, m := range builtinModeNames {
		if name == BuiltinPrefix+m.Name {
			return m.Mode, true
		}
	}
	return "", false
}

// BuiltinDisplayNames 返回全部内置配置的显示名（供 GetConfigFiles 追加）。
func BuiltinDisplayNames() []string {
	names := make([]string, 0, len(builtinModeNames))
	for _, m := range builtinModeNames {
		names = append(names, BuiltinPrefix+m.Name)
	}
	return names
}

// singboxRuleFiles / mihomoRuleFiles 各内核各模式需要的规则文件基础名（不含扩展名）。
// geosite-* → mihomo behavior=domain；geoip-* → behavior=ipcidr。
// sing-box 私网用原生 ip_is_private，不需要 private 规则文件；mihomo 需要。
// fake-ip 模式追加 geosite-fakeipfilter（fakeip 白名单域名走直连 DNS）。
func singboxRuleFiles(mode, dnsMode string) []string {
	files := map[string][]string{
		ModeBypass: {"geosite-cn", "geosite-google", "geoip-cn"},
		ModeBlacklist: {
			"geosite-google", "geosite-gfw", "geosite-greatfire",
			"geoip-facebook", "geoip-fastly", "geoip-google",
			"geoip-netflix", "geoip-telegram", "geoip-twitter",
		},
		ModeGlobal: {},
	}[mode]
	if dnsMode == DNSModeFakeIP {
		files = append(append([]string{}, files...), "geosite-fakeipfilter")
	}
	return files
}

func mihomoRuleFiles(mode, dnsMode string) []string {
	files := map[string][]string{
		ModeBypass: {"geosite-private", "geoip-private", "geosite-cn", "geosite-google", "geoip-cn"},
		ModeBlacklist: {
			"geosite-private", "geoip-private",
			"geosite-google", "geosite-gfw", "geosite-greatfire",
			"geoip-facebook", "geoip-fastly", "geoip-google",
			"geoip-netflix", "geoip-telegram", "geoip-twitter",
		},
		ModeGlobal: {"geosite-private", "geoip-private"},
	}[mode]
	if dnsMode == DNSModeFakeIP {
		// fake-ip-filter 引用 rule-set:geosite-fakeipfilter，需要对应的 rule-provider
		files = append(append([]string{}, files...), "geosite-fakeipfilter")
	}
	return files
}

// builtinRuleFilesAll 两种内核规则文件的并集（供 CheckRuleFiles 校验，宁多勿缺）。
func builtinRuleFilesAll(mode, dnsMode string) []string {
	seen := map[string]bool{}
	var all []string
	for _, tag := range singboxRuleFiles(mode, dnsMode) {
		if !seen[tag] {
			seen[tag] = true
			all = append(all, tag)
		}
	}
	for _, tag := range mihomoRuleFiles(mode, dnsMode) {
		if !seen[tag] {
			seen[tag] = true
			all = append(all, tag)
		}
	}
	return all
}

// isGeositeTag 判断规则 tag 是否为 geosite 类（决定 mihomo rule-provider 的 behavior）。
func isGeositeTag(tag string) bool {
	return len(tag) > 7 && tag[:7] == "geosite"
}

// CheckRuleFiles 校验内置模式所需的规则文件是否齐全（rulesDir 下 srs/ 与 mrs/ 子目录）。
// 返回的错误一次性列出全部缺失文件。
func CheckRuleFiles(mode, dnsMode, rulesDir string) error {
	files := builtinRuleFilesAll(mode, dnsMode)
	if len(files) == 0 {
		return nil
	}
	var missing []string
	for _, f := range files {
		srs := filepath.Join(rulesDir, "srs", f+".srs")
		mrs := filepath.Join(rulesDir, "mrs", f+".mrs")
		if _, err := os.Stat(srs); err != nil {
			missing = append(missing, "run/rules/srs/"+f+".srs")
		}
		if _, err := os.Stat(mrs); err != nil {
			missing = append(missing, "run/rules/mrs/"+f+".mrs")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少规则文件:\n  %s\n请将对应 .srs/.mrs 文件放入上述目录（srs 来源: SagerNet/sing-geosite、SagerNet/sing-geoip；mrs 来源: MetaCubeX/meta-rules-dat）",
			joinLines(missing))
	}
	return nil
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n  "
		}
		out += s
	}
	return out
}

// ─── 私网直连 ────────────────────────────────────────────────────────────────
// sing-box：原生 ip_is_private 字段，不依赖规则文件；
// mihomo：geosite-private / geoip-private 规则集（private.mrs）。

// mihomoIPRule mihomo IP 类规则集的 no-resolve 策略：
// 仅 geoip-cn 不加（域名需解析为 IP 以命中国内 IP 段），其余 IP 规则集都加
// （域名连接跳过 IP 匹配，避免不必要的 DNS 解析）。
func mihomoIPRule(tag, target string) string {
	if tag == "geoip-cn" {
		return "RULE-SET," + tag + "," + target
	}
	return "RULE-SET," + tag + "," + target + ",no-resolve"
}

// ─── 配置生成入口 ─────────────────────────────────────────────────────────────

// BuiltinOptions 内置配置生成参数（运行时开关状态 + Settings.Builtin）。
type BuiltinOptions struct {
	Mode           string
	TunEnabled     bool
	TunStack       string
	TunMTU         int
	TunStrictRoute bool
	ProxyEnabled   bool   // 系统代理开关（决定 mixed inbound / mixed-port）
	ProxyListen    string
	ProxyPort      int
	RulesDir       string           // run/rules 绝对路径
	UIDir          string           // clash-api 的 external-ui 绝对路径（run/ui）
	Cfg            *BuiltinSettings // 可配置参数（nil 时用 DefaultBuiltin）
}

// cfg 返回可配置参数（nil 兜底为默认值）。
func (o *BuiltinOptions) cfg() *BuiltinSettings {
	if o.Cfg == nil {
		d := DefaultBuiltin()
		return &d
	}
	return o.Cfg
}

// BuildBuiltinConfig 按内核生成内置模式配置（JSON for sing-box / YAML for mihomo）。
func BuildBuiltinConfig(core string, opts BuiltinOptions, n *node.Node) ([]byte, error) {
	if !IsBuiltinMode(opts.Mode) {
		return nil, fmt.Errorf("非内置路由模式: %s", opts.Mode)
	}
	if n == nil {
		return nil, fmt.Errorf("内置路由模式需要先应用一个节点")
	}
	if core == CoreMihomo {
		return buildBuiltinMihomo(opts, n)
	}
	return buildBuiltinSingBox(opts, n)
}

// mapLogLevel UI 日志等级 → 内核等级：mihomo 原生 warning；sing-box 是 warn。
func mapLogLevel(level, core string) string {
	if core == CoreSingBox && level == "warning" {
		return "warn"
	}
	return level
}

// ─── sing-box ────────────────────────────────────────────────────────────────

// buildBuiltinSingBox 合成 sing-box JSON 配置。
// DNS 用 1.12 新格式（server 必须带 type）；路由规则对齐 v2rayN 三模板。
func buildBuiltinSingBox(opts BuiltinOptions, n *node.Node) ([]byte, error) {
	b := opts.cfg()

	// proxy outbound：复用节点出站构造（RawOutbound 无损回写优先）
	var proxyOut map[string]interface{}
	if n.RawOutbound != nil {
		proxyOut = n.RawOutbound
	} else {
		var err error
		proxyOut, err = nodeToSingBoxOutbound(*n)
		if err != nil {
			return nil, err
		}
	}
	proxyOut["tag"] = "proxy"

	cfg := map[string]interface{}{
		"log": map[string]interface{}{"level": mapLogLevel(b.LogLevel, CoreSingBox), "timestamp": true},
		"dns": buildBuiltinSingBoxDNS(opts),
		"inbounds":  buildBuiltinSingBoxInbounds(opts),
		"outbounds": []interface{}{proxyOut, map[string]interface{}{"type": "direct", "tag": "direct"}},
		"route":     buildBuiltinSingBoxRoute(opts),
		// clash-api：面板可访问 http://{listen}:{port}/ui
		"experimental": map[string]interface{}{
			"clash_api": map[string]interface{}{
				"external_controller": fmt.Sprintf("%s:%d", b.ClashAPI.Listen, b.ClashAPI.Port),
				"external_ui":         opts.UIDir,
				"secret":              b.ClashAPI.Secret,
				"default_mode":        "rule",
			},
		},
	}
	return marshalJSON(cfg)
}

// singBoxDNSServerMap 把 DNSServer 设置转为 sing-box DNS server 对象。
// 地址必填；端口、路径选填（零值不写入）。
// viaProxy: 代理 DNS 加 detour 出站（直连/解析 DNS 不加，内核报错）；
// withResolver: 直连/代理 DNS 加 domain_resolver 指向解析 DNS（解析 DNS 服务器的域名）。
func singBoxDNSServerMap(tag string, ds DNSServer, viaProxy, withResolver bool) map[string]interface{} {
	m := map[string]interface{}{
		"type":   ds.Type,
		"tag":    tag,
		"server": ds.Address,
	}
	if ds.Port > 0 {
		m["server_port"] = ds.Port
	}
	if ds.Type == "https" && ds.Path != "" {
		m["path"] = ds.Path
	}
	if viaProxy {
		m["detour"] = "proxy"
	}
	if withResolver {
		m["domain_resolver"] = map[string]interface{}{"server": "dns-resolver"}
	}
	return m
}

// buildBuiltinSingBoxDNS 生成 DNS 段（sing-box 1.12 新格式）。
//   - redir-host：按模式分流（bypass: cn→直连；blacklist: gfw/google→代理），其余走 final；
//   - fake-ip：geosite-fakeipfilter 域名走直连 DNS（真实解析），其余 A/AAAA 查询返回 fakeip，
//     非 A/AAAA 查询与未命中白名单的 TXT/HTTPS 等落到 final。
func buildBuiltinSingBoxDNS(opts BuiltinOptions) map[string]interface{} {
	b := opts.cfg()

	resolver := map[string]interface{}{
		"type": "udp", "tag": "dns-resolver", "server": b.ResolverDNS,
	}
	direct := singBoxDNSServerMap("dns-direct", b.SingBoxDirect, false, true)
	proxy := singBoxDNSServerMap("dns-proxy", b.SingBoxProxy, true, true)
	servers := []interface{}{proxy, direct, resolver}

	var rules []interface{}
	final := "dns-proxy"
	if b.DNSMode == DNSModeFakeIP {
		// fakeip 白名单：规则集内域名走直连 DNS 真实解析
		rules = append(rules, map[string]interface{}{
			"rule_set": []string{"geosite-fakeipfilter"},
			"server":   "dns-direct",
		})
		// 其余 A/AAAA 查询返回 fakeip；域名靠 sniff 命中路由规则
		fakeip := map[string]interface{}{
			"type":        "fakeip",
			"tag":         "dns-fakeip",
			"inet4_range": "198.18.0.0/15",
		}
		if b.IPv6 {
			fakeip["inet6_range"] = "fc00::/18"
		}
		servers = append(servers, fakeip)
		rules = append(rules, map[string]interface{}{
			"action":     "route",
			"query_type": []string{"A", "AAAA"},
			"server":     "dns-fakeip",
		})
	} else {
		// redir-host：按模式分流
		switch opts.Mode {
		case ModeBypass:
			// 国内域名走直连 DNS，其余走代理 DNS
			rules = append(rules,
				map[string]interface{}{"rule_set": []string{"geosite-cn"}, "server": "dns-direct"},
			)
		case ModeBlacklist:
			// 被墙/Google 域名走代理 DNS，其余直连
			rules = append(rules,
				map[string]interface{}{"rule_set": []string{"geosite-gfw", "geosite-greatfire", "geosite-google"}, "server": "dns-proxy"},
			)
			final = "dns-direct"
		case ModeGlobal:
			// 全部走代理 DNS
		}
	}
	return map[string]interface{}{
		"servers": servers,
		"rules":   rules,
		"final":   final,
	}
}

// buildBuiltinSingBoxInbounds 按开关生成 mixed / tun inbound。
func buildBuiltinSingBoxInbounds(opts BuiltinOptions) []interface{} {
	inbounds := []interface{}{} // 保持空数组而非 null，避免内核解析失败
	if opts.ProxyEnabled {
		listen := opts.ProxyListen
		if listen == "" {
			listen = "127.0.0.1"
		}
		port := opts.ProxyPort
		if port <= 0 {
			port = 2080
		}
		inbounds = append(inbounds, map[string]interface{}{
			"type":        "mixed",
			"tag":         "mixed-in",
			"listen":      listen,
			"listen_port": port,
		})
	}
	if opts.TunEnabled {
		tun := buildTunInbound(opts.TunStack, opts.TunMTU, opts.TunStrictRoute)
		if opts.cfg().IPv6 {
			// IPv6 开启：TUN 地址补充 IPv6 段
			tun["address"] = append(toIfaceList(tun["address"]), "fdfe:dcba:9876::1/126")
		}
		inbounds = append(inbounds, tun)
	}
	return inbounds
}

// toIfaceList 把 address 字段（[]interface{} 或 []string）统一为 []interface{}。
func toIfaceList(v interface{}) []interface{} {
	switch l := v.(type) {
	case []interface{}:
		return l
	case []string:
		out := make([]interface{}, 0, len(l))
		for _, s := range l {
			out = append(out, s)
		}
		return out
	}
	return []interface{}{}
}

// buildBuiltinSingBoxRoute 生成 route 段：规则对齐 v2rayN 模板，geosite/geoip 走本地 rule_set。
func buildBuiltinSingBoxRoute(opts BuiltinOptions) map[string]interface{} {
	b := opts.cfg()
	rules := []interface{}{
		// 首条嗅探：mixed inbound 的流量只有嗅探后才有域名信息，否则 geosite 规则不命中
		map[string]interface{}{"action": "sniff"},
	}
	if opts.TunEnabled {
		rules = append(rules, map[string]interface{}{"port": 53, "action": "hijack-dns"})
	}

	privateDirect := map[string]interface{}{"ip_is_private": true, "outbound": "direct"}
	udpQUICReject := map[string]interface{}{"port": 443, "network": []string{"udp"}, "action": "reject"}

	final := "proxy"
	switch opts.Mode {
	case ModeBypass:
		rules = append(rules,
			udpQUICReject,
			map[string]interface{}{"rule_set": []string{"geosite-google"}, "outbound": "proxy"},
			privateDirect,
			map[string]interface{}{"rule_set": []string{"geosite-cn"}, "outbound": "direct"},
			map[string]interface{}{"rule_set": []string{"geoip-cn"}, "outbound": "direct"},
		)
	case ModeBlacklist:
		final = "direct"
		rules = append(rules,
			// 对齐 v2rayN black 模板（mihomo 无 protocol 匹配，此规则仅 sing-box 有）
			map[string]interface{}{"protocol": []string{"bittorrent"}, "outbound": "direct"},
			map[string]interface{}{"domain": []string{"api.ip.sb"}, "outbound": "proxy"},
			udpQUICReject,
			map[string]interface{}{"rule_set": []string{"geosite-google"}, "outbound": "proxy"},
			privateDirect,
			map[string]interface{}{"rule_set": []string{"geoip-facebook", "geoip-fastly", "geoip-google", "geoip-netflix", "geoip-telegram", "geoip-twitter"}, "outbound": "proxy"},
			map[string]interface{}{"rule_set": []string{"geosite-gfw", "geosite-greatfire"}, "outbound": "proxy"},
		)
	case ModeGlobal:
		rules = append(rules, udpQUICReject, privateDirect)
	}

	// 本地 rule_set（引用 run/rules/srs/ 下的 .srs 文件）
	var ruleSet []interface{}
	for _, tag := range singboxRuleFiles(opts.Mode, b.DNSMode) {
		ruleSet = append(ruleSet, map[string]interface{}{
			"type":   "local",
			"tag":    tag,
			"format": "binary",
			"path":   filepath.Join(opts.RulesDir, "srs", tag+".srs"),
		})
	}

	route := map[string]interface{}{
		"rules": rules,
		"final": final,
		// 域名默认解析器指向解析 DNS（sing-box 1.12 必需字段，缺省时域名出站解析无依据）
		"default_domain_resolver": map[string]interface{}{
			"server":   "dns-resolver",
			"strategy": ipv6Strategy(b.IPv6),
		},
	}
	if ruleSet != nil {
		route["rule_set"] = ruleSet
	}
	if opts.TunEnabled {
		route["auto_detect_interface"] = true
	}
	return route
}

// ipv6Strategy IPv6 开关对应的解析策略：开 = 双栈优先 IPv4，关 = 仅 IPv4。
func ipv6Strategy(ipv6 bool) string {
	if ipv6 {
		return "prefer_ipv4"
	}
	return "ipv4_only"
}

// ─── mihomo ──────────────────────────────────────────────────────────────────

// buildBuiltinMihomo 合成 mihomo YAML 配置（与 sing-box 版逐条对齐；
// 唯一差异：mihomo 没有 protocol 匹配，bittorrent 直连规则仅 sing-box 生成）。
func buildBuiltinMihomo(opts BuiltinOptions, n *node.Node) ([]byte, error) {
	b := opts.cfg()

	// proxy 条目：RawClashProxy 无损回写优先
	var proxy map[string]interface{}
	if n.RawClashProxy != nil {
		proxy = cloneMap(n.RawClashProxy)
		if proxy == nil {
			return nil, fmt.Errorf("节点原始 Clash 数据无效")
		}
	} else {
		var err error
		proxy, err = node.NodeToClashProxy(*n)
		if err != nil {
			return nil, err
		}
	}
	proxy["name"] = mihomoProxyName

	cfg := map[string]interface{}{
		"mode":      "rule",
		"log-level": mapLogLevel(b.LogLevel, CoreMihomo),
		// clash-api：面板可访问 http://{listen}:{port}/ui
		"external-controller": fmt.Sprintf("%s:%d", b.ClashAPI.Listen, b.ClashAPI.Port),
		"external-ui":         opts.UIDir,
		"secret":              b.ClashAPI.Secret,
		// IPv6：全局 + DNS 两处开关
		"ipv6":        b.IPv6,
		"proxies":     []interface{}{proxy},
		"proxy-groups": []interface{}{map[string]interface{}{"name": mihomoGroupName, "type": "select", "proxies": []interface{}{mihomoProxyName, "DIRECT"}}},
		"dns":         buildBuiltinMihomoDNS(opts),
		"sniffer":     buildBuiltinMihomoSniffer(),
	}
	appendBuiltinMihomoMixed(cfg, opts)
	if opts.TunEnabled {
		cfg["tun"] = buildBuiltinMihomoTun(opts)
	}
	appendBuiltinMihomoRoute(cfg, opts)
	return marshalYAML(cfg)
}

// buildBuiltinMihomoDNS 生成 DNS 段（redir-host / fake-ip）。
//   - 绕过大陆：默认 nameserver = 代理 DNS（#PROXY 经代理组出站，避免 UDP 53 直连被污染），
//     直连域名规则集（geosite-cn / geosite-private）→ 直连 DNS；
//   - GFW列表：默认 nameserver = 直连 DNS，
//     代理域名规则集（geosite-gfw / greatfire / google）→ 代理 DNS；
//   - 全局：默认 nameserver = 代理 DNS（全部流量走代理），无 policy。
//   - fake-ip：以上结构不变，enhanced-mode 切 fake-ip，
//     fake-ip-filter = rule-set:geosite-fakeipfilter（白名单域名真实解析，其余返回 fakeip）。
func buildBuiltinMihomoDNS(opts BuiltinOptions) map[string]interface{} {
	b := opts.cfg()
	directDNS := toIfaceStrings(b.MihomoDirect)
	proxyDNS := toIfaceStrings(withProxySuffix(b.MihomoProxy))

	dns := map[string]interface{}{
		"enable":                  true,
		"ipv6":                    b.IPv6,
		"nameserver":              directDNS,
		"proxy-server-nameserver": []interface{}{b.ResolverDNS},
		"default-nameserver":      []interface{}{b.ResolverDNS},
	}
	if b.DNSMode == DNSModeFakeIP {
		dns["enhanced-mode"] = DNSModeFakeIP
		dns["fake-ip-range"] = "198.18.0.1/16"
		// 白名单：规则集内域名真实解析，其余返回 fakeip
		dns["fake-ip-filter"] = []interface{}{"rule-set:geosite-fakeipfilter"}
		dns["fake-ip-filter-mode"] = "blacklist"
	} else {
		dns["enhanced-mode"] = DNSModeRedirHost
	}

	var policy map[string]interface{}
	switch opts.Mode {
	case ModeBypass:
		dns["nameserver"] = proxyDNS
		policy = map[string]interface{}{
			"rule-set:geosite-private": directDNS,
			"rule-set:geosite-cn":      directDNS,
		}
	case ModeBlacklist:
		policy = map[string]interface{}{
			"rule-set:geosite-google":    proxyDNS,
			"rule-set:geosite-gfw":       proxyDNS,
			"rule-set:geosite-greatfire": proxyDNS,
		}
	case ModeGlobal:
		// 全部走代理：默认 nameserver 也用代理 DNS，无代理域名规则集可写进策略
		dns["nameserver"] = proxyDNS
	}
	if len(policy) > 0 {
		dns["nameserver-policy"] = policy
	}
	return dns
}

// toIfaceStrings []string → []interface{}。
func toIfaceStrings(ss []string) []interface{} {
	out := make([]interface{}, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// withProxySuffix mihomo 代理 DNS 追加 #PROXY 后缀（查询经代理组出站）。
func withProxySuffix(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !strings.Contains(s, "#") {
			s += "#PROXY"
		}
		out = append(out, s)
	}
	return out
}

// buildBuiltinMihomoSniffer 域名嗅探（三种模式固定）：让 TUN/redir-host 下的
// 连接获得真实域名，域名类规则（RULE-SET geosite-* 等）才能命中。
func buildBuiltinMihomoSniffer() map[string]interface{} {
	return map[string]interface{}{
		"enable": true,
		"sniff": map[string]interface{}{
			"HTTP": map[string]interface{}{
				"ports":                []interface{}{80, "8080-8880"},
				"override-destination": true,
			},
			"TLS":  map[string]interface{}{"ports": []interface{}{443, 8443}},
			"QUIC": map[string]interface{}{"ports": []interface{}{443, 8443}},
		},
	}
}

// buildBuiltinMihomoTun TUN 配置（对齐 SetTunMihomo 写入的字段）。
func buildBuiltinMihomoTun(opts BuiltinOptions) map[string]interface{} {
	stack := opts.TunStack
	if stack != "gvisor" && stack != "system" && stack != "mixed" {
		stack = "gvisor"
	}
	mtu := opts.TunMTU
	if mtu <= 0 {
		mtu = 9000
	}
	return map[string]interface{}{
		"enable":                true,
		"stack":                 stack,
		"mtu":                   mtu,
		"auto-route":            true,
		"auto-detect-interface": true,
		"dns-hijack":            []interface{}{"any:53"},
	}
}

// appendBuiltinMihomoMixed 按系统代理开关写 mixed-port / allow-lan（对齐 SetMixedInboundMihomo）。
func appendBuiltinMihomoMixed(cfg map[string]interface{}, opts BuiltinOptions) {
	if !opts.ProxyEnabled {
		return
	}
	port := opts.ProxyPort
	if port <= 0 {
		port = 2080
	}
	cfg["mixed-port"] = port
	allowLan := opts.ProxyListen != "127.0.0.1"
	cfg["allow-lan"] = allowLan
	if allowLan {
		cfg["bind-address"] = "*"
	}
}

// appendBuiltinMihomoRoute 写 rule-providers（file 型 .mrs）与 rules（对齐 sing-box 版顺序）。
func appendBuiltinMihomoRoute(cfg map[string]interface{}, opts BuiltinOptions) {
	dnsMode := opts.cfg().DNSMode
	// rule-providers：仅生成当前模式引用的条目
	providers := map[string]interface{}{}
	for _, tag := range mihomoRuleFiles(opts.Mode, dnsMode) {
		behavior := "ipcidr"
		if isGeositeTag(tag) {
			behavior = "domain"
		}
		providers[tag] = map[string]interface{}{
			"type":     "file",
			"behavior": behavior,
			"format":   "mrs",
			"path":     filepath.Join(opts.RulesDir, "mrs", tag+".mrs"),
		}
	}
	if len(providers) > 0 {
		cfg["rule-providers"] = providers
	}

	// 私网直连：private 规则集（geosite 匹配局域网域名，geoip 匹配私网 IP）
	privateDirect := []interface{}{
		"RULE-SET,geosite-private,DIRECT",
		mihomoIPRule("geoip-private", "DIRECT"),
	}

	udpQUICReject := "AND,((NETWORK,udp),(DST-PORT,443)),REJECT"
	var rules []interface{}
	switch opts.Mode {
	case ModeBypass:
		rules = append(rules,
			udpQUICReject,
			"RULE-SET,geosite-google,"+mihomoGroupName,
		)
		rules = append(rules, privateDirect...)
		rules = append(rules,
			"RULE-SET,geosite-cn,DIRECT",
			mihomoIPRule("geoip-cn", "DIRECT"),
			"MATCH,"+mihomoGroupName,
		)
	case ModeBlacklist:
		rules = append(rules,
			"DOMAIN,api.ip.sb,"+mihomoGroupName,
			udpQUICReject,
			"RULE-SET,geosite-google,"+mihomoGroupName,
		)
		rules = append(rules, privateDirect...)
		for _, tag := range []string{"geoip-facebook", "geoip-fastly", "geoip-google", "geoip-netflix", "geoip-telegram", "geoip-twitter"} {
			rules = append(rules, mihomoIPRule(tag, "PROXY"))
		}
		rules = append(rules,
			"RULE-SET,geosite-gfw,PROXY",
			"RULE-SET,geosite-greatfire,PROXY",
			"MATCH,DIRECT",
		)
	case ModeGlobal:
		rules = append(rules, udpQUICReject)
		rules = append(rules, privateDirect...)
		rules = append(rules, "MATCH,"+mihomoGroupName)
	}
	cfg["rules"] = rules
}
