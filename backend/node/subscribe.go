package node

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FetchSubscription fetches and parses a subscription URL.
// userAgent / timeoutSec 由应用设置提供（默认 clash.meta / 30s）。
func FetchSubscription(rawURL, userAgent string, timeoutSec int) ([]Node, error) {
	if userAgent == "" {
		userAgent = "clash.meta"
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("订阅请求返回状态码 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	nodes, err := ParseContent(string(body))
	if err != nil {
		return nil, fmt.Errorf("解析失败: %v", err)
	}
	for i := range nodes {
		nodes[i].SubURL = rawURL
	}
	return nodes, nil
}

// ─── sing-box JSON ────────────────────────────────────────────────────────────

// singboxOutbound: typed view of a sing-box outbound (used to build editable Node configs)
type singboxOutbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	UUID       string `json:"uuid,omitempty"`
	Password   string `json:"password,omitempty"`
	Method     string `json:"method,omitempty"`

	// VMess
	AlterID  interface{} `json:"alter_id,omitempty"`
	Security string      `json:"security,omitempty"`

	// VLESS
	Flow string `json:"flow,omitempty"`

	// Shadowsocks plugin
	Plugin     string `json:"plugin,omitempty"`
	PluginOpts string `json:"plugin_opts,omitempty"`

	// Hysteria2 / TUIC / Hysteria v1
	UpMbps            int         `json:"up_mbps,omitempty"`
	DownMbps          int         `json:"down_mbps,omitempty"`
	AuthStr           string      `json:"auth_str,omitempty"`
	Obfs              interface{} `json:"obfs,omitempty"` // v1: string; v2: object
	CongestionControl string      `json:"congestion_control,omitempty"`
	UDPRelayMode      string      `json:"udp_relay_mode,omitempty"`

	// Socks / HTTP
	Version  string      `json:"version,omitempty"`
	Username string      `json:"username,omitempty"`
	TLS      *singboxTLS `json:"tls,omitempty"`

	// Transport block (vmess/vless/trojan)
	Transport *singboxTransport `json:"transport,omitempty"`
}

type singboxTLS struct {
	Enabled    bool     `json:"enabled,omitempty"`
	ServerName string   `json:"server_name,omitempty"`
	Insecure   bool     `json:"insecure,omitempty"`
	ALPN       []string `json:"alpn,omitempty"`
	UTLS       *struct {
		Enabled     bool   `json:"enabled,omitempty"`
		Fingerprint string `json:"fingerprint,omitempty"`
	} `json:"utls,omitempty"`
	Reality *struct {
		Enabled   bool   `json:"enabled,omitempty"`
		PublicKey string `json:"public_key,omitempty"`
		ShortID   string `json:"short_id,omitempty"`
	} `json:"reality,omitempty"`
	ECH *struct {
		Enabled bool   `json:"enabled,omitempty"`
		Config  string `json:"config,omitempty"`
	} `json:"ech,omitempty"`
}

type singboxTransport struct {
	Type        string            `json:"type,omitempty"`
	Path        string            `json:"path,omitempty"`
	Host        interface{}       `json:"host,omitempty"` // string (httpupgrade) or []string (http)
	Headers     map[string]string `json:"headers,omitempty"`
	ServiceName string            `json:"service_name,omitempty"`

	MaxEarlyData        int    `json:"max_early_data,omitempty"`
	EarlyDataHeaderName string `json:"early_data_header_name,omitempty"`
}

// toTransportConfig converts a parsed sing-box transport block into our TransportConfig.
func (t *singboxTransport) toTransportConfig() *TransportConfig {
	if t == nil || t.Type == "" {
		return nil
	}
	out := &TransportConfig{
		Type:                t.Type,
		Path:                t.Path,
		ServiceName:         t.ServiceName,
		MaxEarlyData:        t.MaxEarlyData,
		EarlyDataHeaderName: t.EarlyDataHeaderName,
	}
	switch t.Type {
	case "ws":
		// Host lives in headers["Host"] for ws
		if t.Headers != nil {
			if h, ok := t.Headers["Host"]; ok {
				out.Host = h
			} else if h, ok := t.Headers["host"]; ok {
				out.Host = h
			}
		}
	case "xhttp":
		// Fork layouts vary: headers["Host"] (ws-like) or top-level host string/array (Xray)
		if t.Headers != nil {
			if h, ok := t.Headers["Host"]; ok {
				out.Host = h
			} else if h, ok := t.Headers["host"]; ok {
				out.Host = h
			}
		}
		if out.Host == "" {
			switch h := t.Host.(type) {
			case string:
				out.Host = h
			case []interface{}:
				if len(h) > 0 {
					out.Host, _ = h[0].(string)
				}
			}
		}
	case "http":
		// Host is a []string array for http/h2
		switch h := t.Host.(type) {
		case string:
			out.Host = h
		case []interface{}:
			if len(h) > 0 {
				out.Host, _ = h[0].(string)
			}
		}
	case "httpupgrade":
		// Host is a top-level string field
		if h, ok := t.Host.(string); ok {
			out.Host = h
		}
	}
	return out
}

// TLS helper accessors
func (t *singboxTLS) sni() string {
	if t == nil {
		return ""
	}
	return t.ServerName
}

func (t *singboxTLS) alpn() []string {
	if t == nil {
		return nil
	}
	return t.ALPN
}

func (t *singboxTLS) insecure() bool {
	return t != nil && t.Insecure
}

func (t *singboxTLS) fingerprint() string {
	if t == nil || t.UTLS == nil {
		return ""
	}
	return t.UTLS.Fingerprint
}

func (t *singboxTLS) echConfig() string {
	if t == nil || t.ECH == nil || !t.ECH.Enabled {
		return ""
	}
	return t.ECH.Config
}

func (t *singboxTLS) enabled() bool {
	return t != nil && t.Enabled
}

type singboxConfig struct {
	Outbounds []json.RawMessage `json:"outbounds"`
}

// skipOutboundTypes are sing-box built-in non-proxy outbounds that must never
// become nodes. EVERY other outbound type is accepted — structured configs are
// built for well-known types, and everything else (ssh, shadowtls,
// shadowsocksr, mieru, tor, future protocols...) is stored as RawOutbound and
// re-applied verbatim (same approach as v2rayN).
var skipOutboundTypes = map[string]bool{
	"direct": true, "block": true, "dns": true,
	"selector": true, "urltest": true, "": true,
}

func parseSingBoxJSON(content string) ([]Node, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "{") {
		return nil, fmt.Errorf("not json")
	}
	var cfg singboxConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, err
	}
	var nodes []Node
	for _, raw := range cfg.Outbounds {
		// keep the original outbound JSON for lossless round-trip
		var rawMap map[string]interface{}
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			continue
		}
		var ob singboxOutbound
		if err := json.Unmarshal(raw, &ob); err != nil {
			continue
		}
		if skipOutboundTypes[ob.Type] || ob.Server == "" {
			continue
		}

		n := Node{
			ID: newID(), Name: ob.Tag,
			Address: ob.Server, Port: ob.ServerPort,
			Protocol:    ob.Type,
			RawOutbound: rawMap, // verbatim outbound — supports ALL protocols & TLS types
		}
		transport := ob.Transport.toTransportConfig()

		switch ob.Type {
		case "vmess":
			n.VMess = &VMessConfig{
				UUID:        ob.UUID,
				AlterID:     toInt(ob.AlterID),
				Security:    orDefault(ob.Security, "auto"),
				TLS:         ob.TLS.enabled(),
				SNI:         ob.TLS.sni(),
				ALPN:        ob.TLS.alpn(),
				Fingerprint: ob.TLS.fingerprint(),
				Insecure:    ob.TLS.insecure(),
				ECHConfig:   ob.TLS.echConfig(),
				Transport:   transport,
			}
		case "vless":
			n.VLESS = &VLESSConfig{
				UUID:        ob.UUID,
				Flow:        ob.Flow,
				TLS:         ob.TLS.enabled(),
				SNI:         ob.TLS.sni(),
				ALPN:        ob.TLS.alpn(),
				Fingerprint: ob.TLS.fingerprint(),
				Insecure:    ob.TLS.insecure(),
				ECHConfig:   ob.TLS.echConfig(),
				Transport:   transport,
			}
			if ob.TLS != nil && ob.TLS.Reality != nil {
				n.VLESS.PublicKey = ob.TLS.Reality.PublicKey
				n.VLESS.ShortID = ob.TLS.Reality.ShortID
			}
		case "trojan":
			n.Trojan = &TrojanConfig{
				Password:    ob.Password,
				SNI:         ob.TLS.sni(),
				ALPN:        ob.TLS.alpn(),
				Fingerprint: ob.TLS.fingerprint(),
				Insecure:    ob.TLS.insecure(),
				ECHConfig:   ob.TLS.echConfig(),
				Transport:   transport,
			}
		case "shadowsocks":
			n.Protocol = "ss"
			n.SS = &SSConfig{
				Method:     ob.Method,
				Password:   ob.Password,
				Plugin:     ob.Plugin,
				PluginOpts: ob.PluginOpts,
			}
		case "hysteria":
			cfg := &HysteriaConfig{
				AuthStr:  ob.AuthStr,
				SNI:      ob.TLS.sni(),
				Insecure: ob.TLS.insecure(),
				ALPN:     ob.TLS.alpn(),
				UpMbps:   ob.UpMbps,
				DownMbps: ob.DownMbps,
			}
			if s, ok := ob.Obfs.(string); ok {
				cfg.Obfs = s
			}
			n.Hysteria = cfg
		case "hysteria2":
			cfg := &Hysteria2Config{
				Password:  ob.Password,
				SNI:       ob.TLS.sni(),
				Insecure:  ob.TLS.insecure(),
				ALPN:      ob.TLS.alpn(),
				ECHConfig: ob.TLS.echConfig(),
				UpMbps:    ob.UpMbps,
				DownMbps:  ob.DownMbps,
			}
			if ob.Obfs != nil {
				if m, ok := ob.Obfs.(map[string]interface{}); ok {
					cfg.Obfs, _ = m["type"].(string)
					cfg.ObfsPassword, _ = m["password"].(string)
				}
			}
			n.Hysteria2 = cfg
		case "tuic":
			n.TUIC = &TUICConfig{
				UUID:              ob.UUID,
				Password:          ob.Password,
				SNI:               ob.TLS.sni(),
				ALPN:              ob.TLS.alpn(),
				Insecure:          ob.TLS.insecure(),
				CongestionControl: orDefault(ob.CongestionControl, "cubic"),
				UDPRelayMode:      ob.UDPRelayMode,
			}
		case "socks":
			version := orDefault(ob.Version, "5")
			n.Protocol = "socks"
			n.Socks = &SocksConfig{
				Version:  version,
				Username: ob.Username,
				Password: ob.Password,
			}
		case "http":
			n.Protocol = "http"
			n.HTTP = &HTTPConfig{
				Username: ob.Username,
				Password: ob.Password,
				TLS:      ob.TLS.enabled(),
				SNI:      ob.TLS.sni(),
				Insecure: ob.TLS.insecure(),
				ALPN:     ob.TLS.alpn(),
			}
		case "anytls":
			n.AnyTLS = &AnyTLSConfig{
				Password:    ob.Password,
				SNI:         ob.TLS.sni(),
				Insecure:    ob.TLS.insecure(),
				ALPN:        ob.TLS.alpn(),
				Fingerprint: ob.TLS.fingerprint(),
				ECHConfig:   ob.TLS.echConfig(),
			}
		default:
			// ssh / shadowtls / shadowsocksr / wireguard / mieru / tor / ...
			// Node keeps RawOutbound only — re-applied verbatim.
		}
		if n.Name == "" {
			n.Name = fmt.Sprintf("%s-%s:%d", ob.Type, ob.Server, ob.ServerPort)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// ─── Clash YAML ───────────────────────────────────────────────────────────────

type clashConfig struct {
	Proxies []map[string]interface{} `yaml:"proxies"`
}

func parseClashYAML(content string) ([]Node, error) {
	var cfg clashConfig
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, err
	}
	if len(cfg.Proxies) == 0 {
		return nil, fmt.Errorf("no proxies")
	}
	var nodes []Node
	for _, p := range cfg.Proxies {
		n, err := clashProxyToNode(p)
		if err != nil {
			continue
		}
		nodes = append(nodes, *n)
	}
	return nodes, nil
}

func clashProxyToNode(p map[string]interface{}) (*Node, error) {
	t, _ := p["type"].(string)
	name, _ := p["name"].(string)
	server, _ := p["server"].(string)
	port := toInt(p["port"])
	if server == "" || port == 0 {
		return nil, fmt.Errorf("invalid proxy")
	}

	n := &Node{ID: newID(), Name: name, Address: server, Port: port}
	// keep the original Clash proxies entry for lossless re-apply to mihomo configs
	n.RawClashProxy = p

	switch t {
	case "vmess":
		uuid, _ := p["uuid"].(string)
		cipher, _ := p["cipher"].(string)
		alterId := toInt(p["alterId"])
		tls, _ := p["tls"].(bool)
		sni, _ := p["servername"].(string)
		network, _ := p["network"].(string)
		net, err := normalizeNetwork(network)
		if err != nil {
			return nil, err
		}
		transport := clashBuildTransport(net, p)
		fp, _ := p["client-fingerprint"].(string)
		skipVerify, _ := p["skip-cert-verify"].(bool)
		n.Protocol = "vmess"
		n.VMess = &VMessConfig{
			UUID:        uuid,
			AlterID:     alterId,
			Security:    orDefault(cipher, "auto"),
			TLS:         tls,
			SNI:         sni,
			ALPN:        clashStrSlice(p["alpn"]),
			Fingerprint: fp,
			Insecure:    skipVerify,
			Transport:   transport,
		}

	case "vless":
		uuid, _ := p["uuid"].(string)
		network, _ := p["network"].(string)
		tls, _ := p["tls"].(bool)
		sni, _ := p["servername"].(string)
		flow, _ := p["flow"].(string)
		fp, _ := p["client-fingerprint"].(string)
		skipVerify, _ := p["skip-cert-verify"].(bool)
		net, err := normalizeNetwork(network)
		if err != nil {
			return nil, err
		}
		transport := clashBuildTransport(net, p)
		// Reality
		var pubKey, shortID string
		if ro, ok := p["reality-opts"].(map[string]interface{}); ok {
			pubKey, _ = ro["public-key"].(string)
			shortID, _ = ro["short-id"].(string)
			tls = true
		}
		n.Protocol = "vless"
		n.VLESS = &VLESSConfig{
			UUID: uuid, Flow: flow, TLS: tls, SNI: sni,
			ALPN:        clashStrSlice(p["alpn"]),
			Fingerprint: fp, PublicKey: pubKey, ShortID: shortID,
			Insecure:  skipVerify,
			Transport: transport,
		}

	case "trojan":
		password, _ := p["password"].(string)
		sni, _ := p["sni"].(string)
		if sni == "" {
			sni, _ = p["peer"].(string)
		}
		network, _ := p["network"].(string)
		net, err := normalizeNetwork(network)
		if err != nil {
			return nil, err
		}
		transport := clashBuildTransport(net, p)
		fp, _ := p["client-fingerprint"].(string)
		skipVerify, _ := p["skip-cert-verify"].(bool)
		n.Protocol = "trojan"
		n.Trojan = &TrojanConfig{
			Password: password, SNI: sni,
			ALPN:        clashStrSlice(p["alpn"]),
			Fingerprint: fp, Insecure: skipVerify,
			Transport: transport,
		}

	case "ss", "shadowsocks":
		cipher, _ := p["cipher"].(string)
		password, _ := p["password"].(string)
		plugin, _ := p["plugin"].(string)
		var pluginOpts string
		if po, ok := p["plugin-opts"].(map[string]interface{}); ok {
			// convert map to k=v;k=v string (sing-box plugin_opts format)
			var parts []string
			for k, v := range po {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
			pluginOpts = strings.Join(parts, ";")
		}
		n.Protocol = "ss"
		n.SS = &SSConfig{Method: cipher, Password: password, Plugin: plugin, PluginOpts: pluginOpts}

	case "hysteria2", "hy2":
		password, _ := p["password"].(string)
		sni, _ := p["sni"].(string)
		insecure, _ := p["skip-cert-verify"].(bool)
		obfs, _ := p["obfs"].(string)
		obfsPass, _ := p["obfs-password"].(string)
		n.Protocol = "hysteria2"
		n.Hysteria2 = &Hysteria2Config{
			Password: password, SNI: sni, Insecure: insecure,
			ALPN:     clashStrSlice(p["alpn"]),
			UpMbps:   clashFirstInt(p, "up", "upmbps"),
			DownMbps: clashFirstInt(p, "down", "downmbps"),
			Obfs:     obfs, ObfsPassword: obfsPass,
		}

	case "hysteria", "hysteria1":
		auth, _ := p["auth-str"].(string)
		if auth == "" {
			auth, _ = p["auth"].(string)
		}
		sni, _ := p["sni"].(string)
		insecure, _ := p["skip-cert-verify"].(bool)
		obfs, _ := p["obfs"].(string)
		n.Protocol = "hysteria"
		n.Hysteria = &HysteriaConfig{
			AuthStr: auth, SNI: sni, Insecure: insecure,
			ALPN:     clashStrSlice(p["alpn"]),
			UpMbps:   clashFirstInt(p, "up", "upmbps"),
			DownMbps: clashFirstInt(p, "down", "downmbps"),
			Obfs:     obfs,
		}

	case "tuic":
		uuid, _ := p["uuid"].(string)
		password, _ := p["password"].(string)
		sni, _ := p["sni"].(string)
		cc, _ := p["congestion-controller"].(string)
		udpMode, _ := p["udp-relay-mode"].(string)
		insecure, _ := p["skip-cert-verify"].(bool)
		n.Protocol = "tuic"
		n.TUIC = &TUICConfig{
			UUID: uuid, Password: password, SNI: sni,
			ALPN:              clashStrSlice(p["alpn"]),
			CongestionControl: orDefault(cc, "cubic"),
			UDPRelayMode:      udpMode, Insecure: insecure,
		}

	case "socks5", "socks":
		username, _ := p["username"].(string)
		password, _ := p["password"].(string)
		tls, _ := p["tls"].(bool)
		n.Protocol = "socks"
		n.Socks = &SocksConfig{
			Version:  "5",
			Username: username, Password: password,
		}
		if tls {
			// socks over TLS is not a sing-box socks outbound feature;
			// represent it as raw http-less config is not possible — skip TLS flag
		}

	case "http":
		username, _ := p["username"].(string)
		password, _ := p["password"].(string)
		tls, _ := p["tls"].(bool)
		sni, _ := p["sni"].(string)
		insecure, _ := p["skip-cert-verify"].(bool)
		n.Protocol = "http"
		n.HTTP = &HTTPConfig{
			Username: username, Password: password,
			TLS: tls, SNI: sni, Insecure: insecure,
			ALPN: clashStrSlice(p["alpn"]),
		}

	case "anytls":
		password, _ := p["password"].(string)
		sni, _ := p["sni"].(string)
		insecure, _ := p["skip-cert-verify"].(bool)
		n.Protocol = "anytls"
		n.AnyTLS = &AnyTLSConfig{
			Password: password, SNI: sni, Insecure: insecure,
			ALPN:        clashStrSlice(p["alpn"]),
			Fingerprint: stringVal(p["client-fingerprint"]),
		}

	case "ssr":
		n.Protocol = "ssr"
		n.SSR = &SSRConfig{
			Method:        stringVal(p["cipher"]),
			Password:      stringVal(p["password"]),
			Protocol:      stringVal(p["protocol"]),
			ProtocolParam: stringVal(p["protocol-param"]),
			Obfs:          stringVal(p["obfs"]),
			ObfsParam:     stringVal(p["obfs-param"]),
		}

	case "wireguard":
		var reserved []int
		if r := clashIntSlice(p["reserved"]); r != nil {
			reserved = r
		}
		var localAddr []string
		switch v := p["ip"].(type) {
		case string:
			localAddr = append(localAddr, v)
		case []interface{}:
			for _, s := range v {
				localAddr = append(localAddr, fmt.Sprintf("%v", s))
			}
		}
		if ipv6 := stringVal(p["ipv6"]); ipv6 != "" {
			localAddr = append(localAddr, ipv6)
		}
		var dns []string
		switch v := p["dns"].(type) {
		case string:
			dns = append(dns, v)
		case []interface{}:
			for _, s := range v {
				dns = append(dns, fmt.Sprintf("%v", s))
			}
		}
		n.Protocol = "wireguard"
		n.WireGuard = &WireGuardConfig{
			PrivateKey:   stringVal(p["private-key"]),
			PublicKey:    stringVal(p["public-key"]),
			PresharedKey: stringVal(p["pre-shared-key"]),
			Reserved:     reserved,
			LocalAddress: localAddr,
			MTU:          clashFirstInt(p, "mtu"),
			DNS:          dns,
		}

	case "ssh":
		// build raw sing-box ssh outbound from clash fields
		m := map[string]interface{}{
			"type": "ssh", "server": server, "server_port": port,
		}
		if v := stringVal(p["username"]); v != "" {
			m["user"] = v
		}
		if v := stringVal(p["password"]); v != "" {
			m["password"] = v
		}
		n.Protocol = "ssh"
		n.RawOutbound = m

	default:
		return nil, fmt.Errorf("unsupported clash proxy type: %s", t)
	}
	return n, nil
}

// clashBuildTransport parses Clash proxy map transport opts into a TransportConfig.
// Clash uses per-network "*-opts" blocks:
//
//	ws-opts:        { path, headers: {Host}, max-early-data, early-data-header-name }
//	h2-opts:        { host: [], path }
//	grpc-opts:      { grpc-service-name }
//	httpupgrade-opts: { path, host }
//	xhttp-opts:     { path, host: [], mode }
func clashBuildTransport(network string, p map[string]interface{}) *TransportConfig {
	if network == "" {
		return nil
	}
	t := &TransportConfig{Type: network}
	switch network {
	case "ws":
		if opts, ok := p["ws-opts"].(map[string]interface{}); ok {
			t.Path, _ = opts["path"].(string)
			if hdrs, ok := opts["headers"].(map[string]interface{}); ok {
				t.Host, _ = hdrs["Host"].(string)
				if t.Host == "" {
					t.Host, _ = hdrs["host"].(string)
				}
			}
			if ed := toInt(opts["max-early-data"]); ed > 0 {
				t.MaxEarlyData = ed
				t.EarlyDataHeaderName = orDefault(
					stringVal(opts["early-data-header-name"]),
					"Sec-WebSocket-Protocol",
				)
			}
		}
	case "http":
		if opts, ok := p["h2-opts"].(map[string]interface{}); ok {
			t.Path, _ = opts["path"].(string)
			// h2-opts.host is []string
			if hosts, ok := opts["host"].([]interface{}); ok && len(hosts) > 0 {
				t.Host, _ = hosts[0].(string)
			}
		}
	case "grpc":
		if opts, ok := p["grpc-opts"].(map[string]interface{}); ok {
			t.ServiceName, _ = opts["grpc-service-name"].(string)
		}
	case "httpupgrade":
		if opts, ok := p["httpupgrade-opts"].(map[string]interface{}); ok {
			t.Path, _ = opts["path"].(string)
			t.Host, _ = opts["host"].(string)
		}
	case "xhttp":
		if opts, ok := p["xhttp-opts"].(map[string]interface{}); ok {
			t.Path, _ = opts["path"].(string)
			// xhttp-opts.host is []string
			if hosts, ok := opts["host"].([]interface{}); ok && len(hosts) > 0 {
				t.Host, _ = hosts[0].(string)
			}
			t.Mode, _ = opts["mode"].(string)
		}
	}
	return t
}

// ─── Clash helpers ────────────────────────────────────────────────────────────

func stringVal(v interface{}) string {
	s, _ := v.(string)
	return s
}

func clashFirstInt(p map[string]interface{}, keys ...string) int {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			// values like "50 mbps" / 50
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case string:
				fields := strings.Fields(val)
				if len(fields) > 0 {
					n, _ := strconv.Atoi(fields[0])
					return n
				}
			}
		}
	}
	return 0
}

// clashStrSlice converts a Clash YAML string or []string into []string.
func clashStrSlice(v interface{}) []string {
	switch val := v.(type) {
	case string:
		return parseALPN(val)
	case []interface{}:
		var out []string
		for _, s := range val {
			if str, ok := s.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// clashIntSlice converts a Clash YAML int or []int into []int.
func clashIntSlice(v interface{}) []int {
	switch val := v.(type) {
	case int:
		return []int{val}
	case []interface{}:
		var out []int
		for _, s := range val {
			out = append(out, toInt(s))
		}
		return out
	}
	return nil
}
