package node

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ParseContent tries to parse nodes from raw content.
// Priority: sing-box JSON → Clash YAML → base64-decoded URI list → raw URI list
func ParseContent(content string) ([]Node, error) {
	content = strings.TrimSpace(content)

	if nodes, err := parseSingBoxJSON(content); err == nil && len(nodes) > 0 {
		return nodes, nil
	}
	if nodes, err := parseClashYAML(content); err == nil && len(nodes) > 0 {
		return nodes, nil
	}
	if decoded, err := base64Decode(content); err == nil {
		if nodes, err := parseURILines(splitLines(decoded)); err == nil && len(nodes) > 0 {
			return nodes, nil
		}
	}
	return parseURILines(splitLines(content))
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func parseURILines(lines []string) ([]Node, error) {
	var nodes []Node
	for _, line := range lines {
		n, err := ParseURI(line)
		if err != nil {
			continue
		}
		nodes = append(nodes, *n)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("没有找到可解析的节点")
	}
	return nodes, nil
}

// ParseURI parses a single proxy URI into a Node.
func ParseURI(uri string) (*Node, error) {
	uri = strings.TrimSpace(uri)
	switch {
	case strings.HasPrefix(uri, "vmess://"):
		return parseVMess(uri)
	case strings.HasPrefix(uri, "vless://"):
		return parseVLESS(uri)
	case strings.HasPrefix(uri, "trojan://"):
		return parseTrojan(uri)
	case strings.HasPrefix(uri, "ss://"):
		return parseSS(uri)
	case strings.HasPrefix(uri, "hysteria2://"), strings.HasPrefix(uri, "hy2://"):
		return parseHysteria2(uri)
	case strings.HasPrefix(uri, "tuic://"):
		return parseTUIC(uri)
	case strings.HasPrefix(uri, "hysteria://"):
		return parseHysteria(uri)
	case strings.HasPrefix(uri, "socks://"):
		return parseSocks(uri)
	case strings.HasPrefix(uri, "anytls://"):
		return parseAnyTLS(uri)
	case strings.HasPrefix(uri, "ssr://"):
		return parseSSR(uri)
	case strings.HasPrefix(uri, "wireguard://"), strings.HasPrefix(uri, "wg://"):
		return parseWireGuard(uri)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", uri[:min(20, len(uri))])
	}
}

func newID() string { return uuid.New().String() }

func base64Decode(s string) (string, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	pad := len(s) % 4
	if pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// normalizeNetwork maps URI "type" / vmess-json "net" values to sing-box transport type names.
// sing-box does NOT use "h2" — it uses "http" for HTTP/2.
// sing-box does NOT have "tcp" transport — omit transport block when tcp/raw/"".
// xhttp (Xray transport, supported by sing-box forks) is accepted and written per Xray spec.
// kcp is Xray-only and is rejected (sing-box has no such transport).
func normalizeNetwork(net string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(net)) {
	case "", "tcp", "raw", "none":
		return "", nil // tcp / raw / "" → no transport block
	case "h2", "http":
		return "http", nil
	case "ws":
		return "ws", nil
	case "grpc", "gun":
		return "grpc", nil
	case "httpupgrade":
		return "httpupgrade", nil
	case "quic":
		return "quic", nil
	case "xhttp", "splithttp":
		return "xhttp", nil
	case "kcp":
		return "", fmt.Errorf("sing-box 不支持传输层 %q (仅 Xray 支持)", net)
	default:
		return "", fmt.Errorf("未知传输层类型: %q", net)
	}
}

// ─── VMess (legacy base64-JSON format) ───────────────────────────────────────

type vmessJSON struct {
	V    string      `json:"v"`
	PS   string      `json:"ps"`
	Add  string      `json:"add"`
	Port interface{} `json:"port"`
	ID   string      `json:"id"`
	Aid  interface{} `json:"aid"`
	Scy  string      `json:"scy"`
	Net  string      `json:"net"`
	Type string      `json:"type"`   // header type (not used in sing-box)
	Host string      `json:"host"`   // ws Host / http host / grpc authority
	Path string      `json:"path"`   // ws path / http path / grpc service name
	TLS  string      `json:"tls"`    // "tls" | ""
	SNI  string      `json:"sni"`
	ALPN string      `json:"alpn"`
	FP   string      `json:"fp"`  // uTLS fingerprint (newer vmess QR)
	// insecure flags from various exporters
	VerifyCert   *bool        `json:"verify_cert,omitempty"`
	AllowInsecure string      `json:"allowInsecure,omitempty"`
	// early data (some exporters)
	ED  interface{} `json:"ed"` // max_early_data
	// ECH (some newer exporters)
	ECH string `json:"ech,omitempty"`
}

func parseVMess(uri string) (*Node, error) {
	encoded := strings.TrimPrefix(uri, "vmess://")
	// strip fragment
	if idx := strings.Index(encoded, "#"); idx >= 0 {
		encoded = encoded[:idx]
	}
	decoded, err := base64Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("vmess decode error: %v", err)
	}
	var v vmessJSON
	if err := json.Unmarshal([]byte(decoded), &v); err != nil {
		return nil, fmt.Errorf("vmess json error: %v", err)
	}

	network, err := normalizeNetwork(v.Net)
	if err != nil {
		return nil, err
	}
	transport := buildTransportFromVMessJSON(network, v)

	insecure := false
	if v.VerifyCert != nil {
		insecure = !*v.VerifyCert
	}
	if v.AllowInsecure == "1" || strings.EqualFold(v.AllowInsecure, "true") {
		insecure = true
	}

	n := &Node{
		ID:       newID(),
		Name:     v.PS,
		Protocol: "vmess",
		Address:  v.Add,
		Port:     toInt(v.Port),
		VMess: &VMessConfig{
			UUID:        v.ID,
			AlterID:     toInt(v.Aid),
			Security:    orDefault(v.Scy, "auto"),
			TLS:         v.TLS == "tls",
			SNI:         orEmpty(v.SNI, v.Host), // SNI falls back to host for older links
			ALPN:        parseALPN(v.ALPN),
			Fingerprint: v.FP,
			Insecure:    insecure,
			ECHConfig:   v.ECH,
			Transport:   transport,
		},
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("VMess-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

func buildTransportFromVMessJSON(network string, v vmessJSON) *TransportConfig {
	if network == "" {
		return nil
	}
	t := &TransportConfig{Type: network}
	switch network {
	case "ws", "xhttp":
		t.Path = v.Path
		t.Host = v.Host
		if ed := toInt(v.ED); ed > 0 {
			t.MaxEarlyData = ed
			t.EarlyDataHeaderName = "Sec-WebSocket-Protocol"
		}
	case "http":
		t.Path = v.Path
		t.Host = v.Host
	case "grpc":
		// vmess JSON uses "path" for gRPC service name
		t.ServiceName = v.Path
	case "httpupgrade":
		t.Path = v.Path
		t.Host = v.Host
	}
	return t
}

// ─── VLESS (URI format) ───────────────────────────────────────────────────────
// Reference: https://github.com/XTLS/Xray-core/discussions/716
// Key query params: type, security, sni, fp, alpn, flow, path, host,
//   serviceName (grpc), ed (ws early data), pbk (reality pubkey), sid, spx,
//   ech (ECH config list), insecure / allowInsecure

func parseVLESS(uri string) (*Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	name, _ := url.QueryUnescape(u.Fragment)
	network, err := normalizeNetwork(q.Get("type"))
	if err != nil {
		return nil, err
	}
	transport := buildTransportFromQuery(network, q)

	security := q.Get("security")
	hasTLS := security == "tls" || security == "reality"

	cfg := &VLESSConfig{
		UUID:        u.User.Username(),
		Flow:        q.Get("flow"),
		TLS:         hasTLS,
		SNI:         q.Get("sni"),
		ALPN:        parseALPN(q.Get("alpn")),
		Fingerprint: q.Get("fp"),
		Insecure:    queryBool(q, "insecure") || queryBool(q, "allowInsecure"),
		ECHConfig:   q.Get("ech"),
		PublicKey:   q.Get("pbk"),
		ShortID:     q.Get("sid"),
		Transport:   transport,
	}

	n := &Node{
		ID: newID(), Name: name, Protocol: "vless",
		Address: u.Hostname(), Port: port, VLESS: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("VLESS-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── Trojan (URI format) ──────────────────────────────────────────────────────

func parseTrojan(uri string) (*Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	name, _ := url.QueryUnescape(u.Fragment)
	network, err := normalizeNetwork(q.Get("type"))
	if err != nil {
		return nil, err
	}
	transport := buildTransportFromQuery(network, q)

	// peer is the historical alias for sni
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}

	cfg := &TrojanConfig{
		Password:    u.User.Username(),
		SNI:         sni,
		ALPN:        parseALPN(q.Get("alpn")),
		Fingerprint: q.Get("fp"),
		Insecure:    queryBool(q, "insecure") || queryBool(q, "allowInsecure"),
		ECHConfig:   q.Get("ech"),
		Transport:   transport,
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "trojan",
		Address: u.Hostname(), Port: port, Trojan: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("Trojan-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── Shadowsocks (URI format) ─────────────────────────────────────────────────
// Format 1: ss://BASE64(method:password)@host:port#name
// Format 2: ss://BASE64(method:password@host:port)#name  (legacy)
// SIP003 plugin in query: ?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dxxx

func parseSS(uri string) (*Node, error) {
	raw := strings.TrimPrefix(uri, "ss://")
	var name string
	if idx := strings.Index(raw, "#"); idx >= 0 {
		name, _ = url.QueryUnescape(raw[idx+1:])
		raw = raw[:idx]
	}
	// Strip query string (plugin opts sometimes encoded here)
	var query string
	if idx := strings.Index(raw, "?"); idx >= 0 {
		query = raw[idx+1:]
		raw = raw[:idx]
	}

	var method, password, host string
	var port int

	if strings.Contains(raw, "@") {
		parts := strings.SplitN(raw, "@", 2)
		userinfo := parts[0]
		// userinfo may be base64(method:password) or plain method:password
		if decoded, err := base64Decode(userinfo); err == nil && strings.Contains(decoded, ":") {
			userinfo = decoded
		}
		if dec, err := url.QueryUnescape(userinfo); err == nil {
			userinfo = dec
		}
		mp := strings.SplitN(userinfo, ":", 2)
		if len(mp) == 2 {
			method, password = mp[0], mp[1]
		}
		// host:port part
		if u, err := url.Parse("ss://" + parts[1]); err == nil {
			host = u.Hostname()
			port, _ = strconv.Atoi(u.Port())
		}
	} else {
		// entire payload is base64
		decoded, err := base64Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("ss decode error: %v", err)
		}
		if u, err := url.Parse("ss://" + decoded); err == nil {
			method = u.User.Username()
			password, _ = u.User.Password()
			host = u.Hostname()
			port, _ = strconv.Atoi(u.Port())
		}
	}

	cfg := &SSConfig{Method: method, Password: password}
	// SIP003 plugin via query string:
	//   plugin=<name>[;<opt>=<v>]...  (semicolon separated, URL-encoded as %3B)
	// e.g. plugin=obfs-local;obfs=http;obfs-host=example.com
	if query != "" {
		q, _ := url.ParseQuery(query)
		pluginRaw := q.Get("plugin")
		if pluginRaw != "" {
			segs := strings.Split(pluginRaw, ";")
			cfg.Plugin = segs[0]
			if len(segs) > 1 {
				cfg.PluginOpts = strings.Join(segs[1:], ";")
			}
		}
		// some exporters use separate keys
		if cfg.Plugin == "" {
			cfg.Plugin = q.Get("plugin-name")
			cfg.PluginOpts = q.Get("plugin-opts")
		}
	}

	n := &Node{
		ID: newID(), Name: name, Protocol: "ss",
		Address: host, Port: port, SS: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("SS-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── Hysteria2 (URI format) ───────────────────────────────────────────────────
// hysteria2://[password]@host:port?sni=&insecure=&obfs=&obfs-password=&upmbps=&downmbps=&ech=&pinSHA256=

func parseHysteria2(uri string) (*Node, error) {
	uri = strings.Replace(uri, "hy2://", "hysteria2://", 1)
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 443
	}
	name, _ := url.QueryUnescape(u.Fragment)
	insecure, _ := strconv.ParseBool(q.Get("insecure"))
	// The whole userinfo is the password (v2rayN-compatible), may contain ':'
	// and be URL-encoded — e.g. hysteria2://pass%3Aword@host:443
	password := u.User.Username()
	if p, _ := u.User.Password(); p != "" {
		password = password + ":" + p
	}
	if dec, err := url.QueryUnescape(password); err == nil {
		password = dec
	}

	cfg := &Hysteria2Config{
		Password:     password,
		SNI:          q.Get("sni"),
		Insecure:     insecure,
		ALPN:         parseALPN(q.Get("alpn")),
		ECHConfig:    q.Get("ech"),
		UpMbps:       queryFirstInt(q, "upmbps", "up"),
		DownMbps:     queryFirstInt(q, "downmbps", "down"),
		Obfs:         q.Get("obfs"),
		ObfsPassword: q.Get("obfs-password"),
	}
	// pinSHA256 implies self-signed cert pinning — sing-box only has insecure
	if q.Get("pinSHA256") != "" {
		cfg.Insecure = true
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "hysteria2",
		Address: u.Hostname(), Port: port, Hysteria2: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("Hysteria2-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── Hysteria v1 (URI format) ─────────────────────────────────────────────────
// hysteria://host:port?auth=&peer=&insecure=&upmbps=&downmbps=&obfs=&obfsParam=&alpn=

func parseHysteria(uri string) (*Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	name, _ := url.QueryUnescape(u.Fragment)
	insecure, _ := strconv.ParseBool(q.Get("insecure"))

	// auth may be in userinfo or "auth" query param
	auth := q.Get("auth")
	if auth == "" {
		auth = u.User.Username()
		if p, _ := u.User.Password(); p != "" {
			auth = auth + ":" + p
		}
	}

	cfg := &HysteriaConfig{
		AuthStr:  auth,
		SNI:      orDefault(q.Get("peer"), q.Get("sni")),
		Insecure: insecure,
		ALPN:     parseALPN(q.Get("alpn")),
		UpMbps:   queryFirstInt(q, "upmbps", "up"),
		DownMbps: queryFirstInt(q, "downmbps", "down"),
		Obfs:     orDefault(q.Get("obfsParam"), q.Get("obfs")),
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "hysteria",
		Address: u.Hostname(), Port: port, Hysteria: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("Hysteria-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── TUIC (URI format) ────────────────────────────────────────────────────────

func parseTUIC(uri string) (*Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 443
	}
	name, _ := url.QueryUnescape(u.Fragment)
	insecure, _ := strconv.ParseBool(q.Get("allow_insecure"))
	if v, err := strconv.ParseBool(q.Get("insecure")); err == nil {
		insecure = insecure || v
	}
	password, _ := u.User.Password()

	cfg := &TUICConfig{
		UUID:              u.User.Username(),
		Password:          password,
		SNI:               q.Get("sni"),
		ALPN:              parseALPN(q.Get("alpn")),
		Insecure:          insecure,
		CongestionControl: orDefault(q.Get("congestion_control"), "cubic"),
		UDPRelayMode:      q.Get("udp_relay_mode"),
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "tuic",
		Address: u.Hostname(), Port: port, TUIC: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("TUIC-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── Socks (URI format, v2rayN compatible) ────────────────────────────────────
// socks://BASE64(user:pass@host:port)#name  or  socks://user:pass@host:port#name

func parseSocks(uri string) (*Node, error) {
	raw := strings.TrimPrefix(uri, "socks://")
	var name string
	if idx := strings.Index(raw, "#"); idx >= 0 {
		name, _ = url.QueryUnescape(raw[idx+1:])
		raw = raw[:idx]
	}
	// whole payload may be base64
	if !strings.Contains(raw, "@") {
		if decoded, err := base64Decode(raw); err == nil && strings.Contains(decoded, "@") {
			raw = decoded
		}
	}
	atIdx := strings.LastIndex(raw, "@")
	if atIdx < 0 {
		return nil, fmt.Errorf("socks link 格式错误")
	}
	userinfo := raw[:atIdx]
	if decoded, err := base64Decode(userinfo); err == nil && strings.Contains(decoded, ":") {
		userinfo = decoded
	}
	if dec, err := url.QueryUnescape(userinfo); err == nil {
		userinfo = dec
	}
	hostPort := raw[atIdx+1:]
	// last colon separates host:port (host may be IPv6 in brackets handled below)
	var host string
	var port int
	if strings.HasSuffix(hostPort, "]") {
		// [v6]:port
		idx := strings.LastIndex(hostPort, "]:")
		if idx < 0 {
			return nil, fmt.Errorf("socks link 端口错误")
		}
		host = strings.Trim(hostPort[:idx+1], "[]")
		port, _ = strconv.Atoi(hostPort[idx+2:])
	} else {
		idx := strings.LastIndex(hostPort, ":")
		if idx < 0 {
			return nil, fmt.Errorf("socks link 端口错误")
		}
		host = hostPort[:idx]
		port, _ = strconv.Atoi(hostPort[idx+1:])
	}
	up := strings.SplitN(userinfo, ":", 2)
	cfg := &SocksConfig{Version: "5"}
	if len(up) == 2 {
		cfg.Username, cfg.Password = up[0], up[1]
	} else if len(up) == 1 {
		cfg.Username = up[0]
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "socks",
		Address: host, Port: port, Socks: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("SOCKS-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── AnyTLS (URI format) ──────────────────────────────────────────────────────
// anytls://password@host:port?sni=&insecure=&alpn=&fp=&ech=#name

func parseAnyTLS(uri string) (*Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 443
	}
	name, _ := url.QueryUnescape(u.Fragment)
	insecure, _ := strconv.ParseBool(q.Get("insecure"))

	cfg := &AnyTLSConfig{
		Password:    u.User.Username(),
		SNI:         q.Get("sni"),
		Insecure:    insecure,
		ALPN:        parseALPN(q.Get("alpn")),
		Fingerprint: q.Get("fp"),
		ECHConfig:   q.Get("ech"),
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "anytls",
		Address: u.Hostname(), Port: port, AnyTLS: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("AnyTLS-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── ShadowsocksR (URI format) ────────────────────────────────────────────────
// ssr://base64(host:port:protocol:method:obfs:base64(password)/?obfsparam=&protoparam=&remarks=)

func parseSSR(uri string) (*Node, error) {
	raw := strings.TrimPrefix(uri, "ssr://")
	decoded, err := base64Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("ssr decode error: %v", err)
	}
	mainPart := decoded
	queryPart := ""
	if idx := strings.Index(decoded, "/?"); idx >= 0 {
		mainPart = decoded[:idx]
		queryPart = decoded[idx+2:]
	}
	q := url.Values{}
	if queryPart != "" {
		if qp, err := url.ParseQuery(queryPart); err == nil {
			q = qp
		}
	}
	// host:port:protocol:method:obfs:base64(password)
	segs := strings.Split(mainPart, ":")
	if len(segs) < 6 {
		return nil, fmt.Errorf("ssr link 格式错误")
	}
	password64 := strings.Join(segs[5:], ":")
	password, err := base64Decode(password64)
	if err != nil {
		password = password64
	}
	name, _ := url.QueryUnescape(q.Get("remarks"))
	port, _ := strconv.Atoi(segs[1])

	n := &Node{
		ID: newID(), Name: name, Protocol: "ssr",
		Address: segs[0], Port: port,
		SSR: &SSRConfig{
			Method:        segs[3],
			Password:      password,
			Protocol:      segs[2],
			Obfs:          segs[4],
			ProtocolParam: q.Get("protoparam"),
			ObfsParam:     q.Get("obfsparam"),
		},
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("SSR-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── WireGuard (URI format, v2rayN compatible) ────────────────────────────────
// wireguard://privateKey@host:port?publickey=&presharedkey=&reserved=&address=&mtu=&dns=#name

func parseWireGuard(uri string) (*Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	name, _ := url.QueryUnescape(u.Fragment)

	var reserved []int
	resRaw := q.Get("reserved")
	if resRaw != "" {
		if strings.Contains(resRaw, ",") {
			for _, s := range strings.Split(resRaw, ",") {
				if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
					reserved = append(reserved, v)
				}
			}
		} else if b, err := base64.StdEncoding.DecodeString(resRaw); err == nil && len(b) == 3 {
			reserved = []int{int(b[0]), int(b[1]), int(b[2])}
		}
	}
	var localAddr []string
	for _, s := range strings.Split(q.Get("address"), ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			localAddr = append(localAddr, s)
		}
	}
	var dns []string
	for _, s := range strings.Split(q.Get("dns"), ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			dns = append(dns, s)
		}
	}

	cfg := &WireGuardConfig{
		PrivateKey:   u.User.Username(),
		PublicKey:    q.Get("publickey"),
		PresharedKey: q.Get("presharedkey"),
		Reserved:     reserved,
		LocalAddress: localAddr,
		MTU:          queryFirstInt(q, "mtu"),
		DNS:          dns,
	}
	if cfg.PublicKey == "" {
		return nil, fmt.Errorf("wireguard link 缺少 peer publickey")
	}
	n := &Node{
		ID: newID(), Name: name, Protocol: "wireguard",
		Address: u.Hostname(), Port: port, WireGuard: cfg,
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("WG-%s:%d", n.Address, n.Port)
	}
	return n, nil
}

// ─── Transport builder from URI query params ──────────────────────────────────
// Used by VLESS, Trojan (and any future URI-format protocol).
// URI params:
//   ws/httpupgrade/xhttp: path, host
//   ws only:              ed (max_early_data), eh (early_data_header_name)
//   http (h2):            path, host
//   grpc:                 serviceName (primary), path (fallback)
//   xhttp:                mode (auto/packet-up/stream-up/stream-one)

func buildTransportFromQuery(network string, q url.Values) *TransportConfig {
	if network == "" {
		return nil
	}
	t := &TransportConfig{Type: network}
	switch network {
	case "ws":
		t.Path = q.Get("path")
		t.Host = q.Get("host")
		if ed := toInt(q.Get("ed")); ed > 0 {
			t.MaxEarlyData = ed
			t.EarlyDataHeaderName = orDefault(q.Get("eh"), "Sec-WebSocket-Protocol")
		}
	case "xhttp":
		t.Path = q.Get("path")
		t.Host = q.Get("host")
		t.Mode = q.Get("mode")
	case "http":
		t.Path = q.Get("path")
		t.Host = q.Get("host")
	case "grpc":
		// URI uses "serviceName"; some exporters use "path" as fallback
		t.ServiceName = orDefault(q.Get("serviceName"), q.Get("path"))
	case "httpupgrade":
		t.Path = q.Get("path")
		t.Host = q.Get("host")
	case "quic":
		// no extra fields
	}
	return t
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func parseALPN(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		n, _ := strconv.Atoi(val)
		return n
	}
	return 0
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// orEmpty returns s if non-empty, else fallback — used for optional fallback fields.
func orEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// queryBool returns true when the query param equals "1"/"true" (case-insensitive).
func queryBool(q url.Values, key string) bool {
	v := strings.ToLower(strings.TrimSpace(q.Get(key)))
	return v == "1" || v == "true" || v == "yes"
}

// queryFirstInt returns the first non-empty int value among keys.
func queryFirstInt(q url.Values, keys ...string) int {
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			if n, err := strconv.Atoi(strings.TrimSuffix(v, " mbps")); err == nil {
				return n
			}
		}
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
