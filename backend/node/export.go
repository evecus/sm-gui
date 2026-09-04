package node

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// NodeToURI converts a Node back to a share URI (v2rayN-compatible format).
// Raw-only protocols (ssh / shadowtls / ...) have no share-link format and
// return an error.
func NodeToURI(n Node) (string, error) {
	switch n.Protocol {
	case "vmess":
		if n.VMess == nil {
			return "", fmt.Errorf("VMess 配置为空")
		}
		return vmessToURI(n), nil
	case "vless":
		if n.VLESS == nil {
			return "", fmt.Errorf("VLESS 配置为空")
		}
		return vlessToURI(n), nil
	case "trojan":
		if n.Trojan == nil {
			return "", fmt.Errorf("Trojan 配置为空")
		}
		return trojanToURI(n), nil
	case "ss":
		if n.SS == nil {
			return "", fmt.Errorf("Shadowsocks 配置为空")
		}
		return ssToURI(n), nil
	case "hysteria2":
		if n.Hysteria2 == nil {
			return "", fmt.Errorf("Hysteria2 配置为空")
		}
		return hysteria2ToURI(n), nil
	case "hysteria":
		if n.Hysteria == nil {
			return "", fmt.Errorf("Hysteria 配置为空")
		}
		return hysteriaToURI(n), nil
	case "tuic":
		if n.TUIC == nil {
			return "", fmt.Errorf("TUIC 配置为空")
		}
		return tuicToURI(n), nil
	case "socks":
		if n.Socks == nil {
			return "", fmt.Errorf("Socks 配置为空")
		}
		return socksToURI(n), nil
	case "http":
		if n.HTTP == nil {
			return "", fmt.Errorf("HTTP 配置为空")
		}
		return httpToURI(n), nil
	case "anytls":
		if n.AnyTLS == nil {
			return "", fmt.Errorf("AnyTLS 配置为空")
		}
		return anytlsToURI(n), nil
	case "ssr":
		if n.SSR == nil {
			return "", fmt.Errorf("SSR 配置为空")
		}
		return ssrToURI(n), nil
	case "wireguard":
		if n.WireGuard == nil {
			return "", fmt.Errorf("WireGuard 配置为空")
		}
		return wireguardToURI(n), nil
	default:
		return "", fmt.Errorf("协议 %q 无分享链接格式", n.Protocol)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func b64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func b64RawURL(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func frag(name string) string {
	return "#" + url.QueryEscape(name)
}

// transportNetName converts TransportConfig.Type into URI "type" naming.
func transportNetName(t *TransportConfig) string {
	if t == nil || t.Type == "" {
		return "tcp"
	}
	switch t.Type {
	case "http":
		return "h2"
	default:
		return t.Type // ws | grpc | httpupgrade | quic | xhttp
	}
}

// transportURIParams appends path/host/serviceName/ed/mode params for the transport.
func transportURIParams(t *TransportConfig, q *url.Values) {
	if t == nil || t.Type == "" {
		return
	}
	switch t.Type {
	case "ws", "httpupgrade", "http", "xhttp":
		if t.Path != "" {
			q.Set("path", t.Path)
		}
		if t.Host != "" {
			q.Set("host", t.Host)
		}
		if t.Type == "ws" && t.MaxEarlyData > 0 {
			q.Set("ed", strconv.Itoa(t.MaxEarlyData))
		}
		if t.Type == "xhttp" && t.Mode != "" {
			q.Set("mode", t.Mode)
		}
	case "grpc":
		if t.ServiceName != "" {
			q.Set("serviceName", t.ServiceName)
		}
	}
}

// ─── VMess: vmess://BASE64(JSON) ──────────────────────────────────────────────

func vmessToURI(n Node) string {
	v := n.VMess
	net := transportNetName(v.Transport)
	host, path := "", ""
	if v.Transport != nil {
		host = v.Transport.Host
		path = v.Transport.Path
		if v.Transport.Type == "grpc" {
			path = v.Transport.ServiceName
		}
	}
	obj := map[string]interface{}{
		"v":    "2",
		"ps":   n.Name,
		"add":  n.Address,
		"port": n.Port,
		"id":   v.UUID,
		"aid":  v.AlterID,
		"scy":  orDefault(v.Security, "auto"),
		"net":  net,
		"type": "none",
		"host": host,
		"path": path,
		"tls":  boolStr(v.TLS, "tls", ""),
		"sni":  v.SNI,
		"alpn": strings.Join(v.ALPN, ","),
		"fp":   v.Fingerprint,
	}
	if v.Transport != nil && v.Transport.MaxEarlyData > 0 {
		obj["ed"] = v.Transport.MaxEarlyData
	}
	data, _ := json.Marshal(obj)
	return "vmess://" + b64Std(string(data))
}

// ─── VLESS ────────────────────────────────────────────────────────────────────

func vlessToURI(n Node) string {
	v := n.VLESS
	q := url.Values{}
	q.Set("type", transportNetName(v.Transport))
	transportURIParams(v.Transport, &q)

	if v.PublicKey != "" {
		q.Set("security", "reality")
		q.Set("pbk", v.PublicKey)
		q.Set("sid", v.ShortID)
		q.Set("fp", orDefault(v.Fingerprint, "chrome"))
	} else if v.TLS {
		q.Set("security", "tls")
		if v.Fingerprint != "" {
			q.Set("fp", v.Fingerprint)
		}
	}
	if v.TLS && v.SNI != "" {
		q.Set("sni", v.SNI)
	}
	if len(v.ALPN) > 0 {
		q.Set("alpn", strings.Join(v.ALPN, ","))
	}
	if v.Flow != "" {
		q.Set("flow", v.Flow)
	}
	if v.Insecure {
		q.Set("insecure", "1")
	}
	if v.ECHConfig != "" {
		q.Set("ech", v.ECHConfig)
	}
	user := url.User(v.UUID)
	return "vless://" + user.String() + "@" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── Trojan ───────────────────────────────────────────────────────────────────

func trojanToURI(n Node) string {
	t := n.Trojan
	q := url.Values{}
	q.Set("type", transportNetName(t.Transport))
	transportURIParams(t.Transport, &q)
	if t.SNI != "" {
		q.Set("sni", t.SNI)
	}
	if t.Fingerprint != "" {
		q.Set("fp", t.Fingerprint)
	}
	if len(t.ALPN) > 0 {
		q.Set("alpn", strings.Join(t.ALPN, ","))
	}
	if t.Insecure {
		q.Set("allowInsecure", "1")
	}
	if t.ECHConfig != "" {
		q.Set("ech", t.ECHConfig)
	}
	user := url.User(t.Password)
	return "trojan://" + user.String() + "@" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── Shadowsocks: SIP002 ──────────────────────────────────────────────────────

func ssToURI(n Node) string {
	s := n.SS
	userinfo := b64RawURL(s.Method + ":" + s.Password)
	link := "ss://" + userinfo + "@" + hostPort(n.Address, n.Port)
	q := url.Values{}
	if s.Plugin != "" {
		pluginStr := s.Plugin
		if s.PluginOpts != "" {
			pluginStr = pluginStr + ";" + s.PluginOpts
		}
		q.Set("plugin", pluginStr)
	}
	if len(q) > 0 {
		link += "?" + q.Encode()
	}
	return link + frag(n.Name)
}

// ─── Hysteria2 ────────────────────────────────────────────────────────────────

func hysteria2ToURI(n Node) string {
	h := n.Hysteria2
	q := url.Values{}
	if h.SNI != "" {
		q.Set("sni", h.SNI)
	}
	if h.Insecure {
		q.Set("insecure", "1")
	}
	if h.Obfs != "" {
		q.Set("obfs", h.Obfs)
	}
	if h.ObfsPassword != "" {
		q.Set("obfs-password", h.ObfsPassword)
	}
	if h.UpMbps > 0 {
		q.Set("upmbps", strconv.Itoa(h.UpMbps))
	}
	if h.DownMbps > 0 {
		q.Set("downmbps", strconv.Itoa(h.DownMbps))
	}
	if len(h.ALPN) > 0 {
		q.Set("alpn", strings.Join(h.ALPN, ","))
	}
	if h.ECHConfig != "" {
		q.Set("ech", h.ECHConfig)
	}
	user := url.QueryEscape(h.Password)
	return "hysteria2://" + user + "@" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── Hysteria v1 ──────────────────────────────────────────────────────────────

func hysteriaToURI(n Node) string {
	h := n.Hysteria
	q := url.Values{}
	if h.AuthStr != "" {
		q.Set("auth", h.AuthStr)
	}
	if h.SNI != "" {
		q.Set("peer", h.SNI)
	}
	if h.Insecure {
		q.Set("insecure", "1")
	}
	if h.UpMbps > 0 {
		q.Set("upmbps", strconv.Itoa(h.UpMbps))
	}
	if h.DownMbps > 0 {
		q.Set("downmbps", strconv.Itoa(h.DownMbps))
	}
	if h.Obfs != "" {
		q.Set("obfs", h.Obfs)
	}
	if len(h.ALPN) > 0 {
		q.Set("alpn", strings.Join(h.ALPN, ","))
	}
	return "hysteria://" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── TUIC ─────────────────────────────────────────────────────────────────────

func tuicToURI(n Node) string {
	t := n.TUIC
	q := url.Values{}
	if t.SNI != "" {
		q.Set("sni", t.SNI)
	}
	if len(t.ALPN) > 0 {
		q.Set("alpn", strings.Join(t.ALPN, ","))
	}
	q.Set("congestion_control", orDefault(t.CongestionControl, "cubic"))
	if t.UDPRelayMode != "" {
		q.Set("udp_relay_mode", t.UDPRelayMode)
	}
	if t.Insecure {
		q.Set("allow_insecure", "1")
	}
	user := url.UserPassword(t.UUID, t.Password)
	return "tuic://" + user.String() + "@" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── Socks: socks://BASE64(user:pass@host:port) ───────────────────────────────

func socksToURI(n Node) string {
	s := n.Socks
	payload := hostPort(n.Address, n.Port)
	if s.Username != "" {
		payload = s.Username + ":" + s.Password + "@" + payload
	}
	return "socks://" + b64Std(payload) + frag(n.Name)
}

// ─── HTTP(S) proxy ────────────────────────────────────────────────────────────

func httpToURI(n Node) string {
	s := n.HTTP
	scheme := "http"
	if s.TLS {
		scheme = "https"
	}
	link := scheme + "://" + hostPort(n.Address, n.Port)
	if s.Username != "" {
		link = scheme + "://" + url.UserPassword(s.Username, s.Password).String() + "@" + hostPort(n.Address, n.Port)
	}
	q := url.Values{}
	if s.SNI != "" {
		q.Set("sni", s.SNI)
	}
	if s.Insecure {
		q.Set("insecure", "1")
	}
	if len(q) > 0 {
		link += "?" + q.Encode()
	}
	return link + frag(n.Name)
}

// ─── AnyTLS ───────────────────────────────────────────────────────────────────

func anytlsToURI(n Node) string {
	a := n.AnyTLS
	q := url.Values{}
	if a.SNI != "" {
		q.Set("sni", a.SNI)
	}
	if a.Insecure {
		q.Set("insecure", "1")
	}
	if a.Fingerprint != "" {
		q.Set("fp", a.Fingerprint)
	}
	if len(a.ALPN) > 0 {
		q.Set("alpn", strings.Join(a.ALPN, ","))
	}
	if a.ECHConfig != "" {
		q.Set("ech", a.ECHConfig)
	}
	user := url.QueryEscape(a.Password)
	return "anytls://" + user + "@" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── ShadowsocksR ─────────────────────────────────────────────────────────────

func ssrToURI(n Node) string {
	s := n.SSR
	q := url.Values{}
	if s.ObfsParam != "" {
		q.Set("obfsparam", s.ObfsParam)
	}
	if s.ProtocolParam != "" {
		q.Set("protoparam", s.ProtocolParam)
	}
	q.Set("remarks", n.Name)
	payload := hostPort(n.Address, n.Port) + ":" + s.Protocol + ":" + s.Method + ":" + s.Obfs +
		":" + b64Std(s.Password) + "/?" + q.Encode()
	return "ssr://" + b64Std(payload)
}

// ─── WireGuard ────────────────────────────────────────────────────────────────

func wireguardToURI(n Node) string {
	w := n.WireGuard
	q := url.Values{}
	q.Set("publickey", w.PublicKey)
	if w.PresharedKey != "" {
		q.Set("presharedkey", w.PresharedKey)
	}
	if len(w.LocalAddress) > 0 {
		q.Set("address", strings.Join(w.LocalAddress, ","))
	}
	if len(w.Reserved) == 3 {
		q.Set("reserved", fmt.Sprintf("%d,%d,%d", w.Reserved[0], w.Reserved[1], w.Reserved[2]))
	}
	if w.MTU > 0 {
		q.Set("mtu", strconv.Itoa(w.MTU))
	}
	if len(w.DNS) > 0 {
		q.Set("dns", strings.Join(w.DNS, ","))
	}
	user := url.User(w.PrivateKey)
	return "wireguard://" + user.String() + "@" + hostPort(n.Address, n.Port) + "?" + q.Encode() + frag(n.Name)
}

// ─── small helpers ────────────────────────────────────────────────────────────

func boolStr(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func hostPort(host string, port int) string {
	// bracket IPv6 literals
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}
