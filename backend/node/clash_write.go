package node

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// NodeToClashProxy 把 Node 转换为 Clash/mihomo 风格的 proxies 条目
//（name 保留原节点名，写入配置时由调用方改写为固定名 "proxy"）。
// 它是 clashProxyToNode 的逆向转换，字段名遵循 Clash YAML 规范。
//
// 优先级：若节点带有 RawClashProxy（从 Clash YAML 导入），直接使用原始
// 条目（仅刷新 name/server/port），实现无损回写；否则按结构化字段生成。
func NodeToClashProxy(n Node) (map[string]interface{}, error) {
	if n.RawClashProxy != nil {
		p := cloneClashProxy(n.RawClashProxy)
		if p == nil {
			return nil, fmt.Errorf("节点原始 Clash 数据无效")
		}
		p["name"] = n.Name
		p["server"] = n.Address
		p["port"] = n.Port
		return p, nil
	}

	p := map[string]interface{}{
		"name":   n.Name,
		"server": n.Address,
		"port":   n.Port,
	}

	switch n.Protocol {

	case "vmess":
		if n.VMess == nil {
			return nil, fmt.Errorf("VMess 配置为空")
		}
		p["type"] = "vmess"
		p["uuid"] = n.VMess.UUID
		p["alterId"] = n.VMess.AlterID
		p["cipher"] = orDefault(n.VMess.Security, "auto")
		setClashTLS(p, n.VMess.TLS, n.VMess.SNI, n.VMess.ALPN, n.VMess.Insecure, n.VMess.Fingerprint)
		applyClashTransport(p, n.VMess.Transport)

	case "vless":
		if n.VLESS == nil {
			return nil, fmt.Errorf("VLESS 配置为空")
		}
		p["type"] = "vless"
		p["uuid"] = n.VLESS.UUID
		if n.VLESS.Flow != "" {
			p["flow"] = n.VLESS.Flow
		}
		setClashTLS(p, n.VLESS.TLS, n.VLESS.SNI, n.VLESS.ALPN, n.VLESS.Insecure, n.VLESS.Fingerprint)
		if n.VLESS.PublicKey != "" {
			p["tls"] = true
			p["reality-opts"] = map[string]interface{}{
				"public-key": n.VLESS.PublicKey,
				"short-id":   n.VLESS.ShortID,
			}
		}
		applyClashTransport(p, n.VLESS.Transport)

	case "trojan":
		if n.Trojan == nil {
			return nil, fmt.Errorf("Trojan 配置为空")
		}
		p["type"] = "trojan"
		p["password"] = n.Trojan.Password
		if n.Trojan.SNI != "" {
			p["sni"] = n.Trojan.SNI
		}
		if len(n.Trojan.ALPN) > 0 {
			p["alpn"] = n.Trojan.ALPN
		}
		if n.Trojan.Insecure {
			p["skip-cert-verify"] = true
		}
		if n.Trojan.Fingerprint != "" {
			p["client-fingerprint"] = n.Trojan.Fingerprint
		}
		applyClashTransport(p, n.Trojan.Transport)

	case "ss":
		if n.SS == nil {
			return nil, fmt.Errorf("Shadowsocks 配置为空")
		}
		p["type"] = "ss"
		p["cipher"] = n.SS.Method
		p["password"] = n.SS.Password
		if n.SS.Plugin != "" {
			p["plugin"] = n.SS.Plugin
			if n.SS.PluginOpts != "" {
				if opts := parsePluginOpts(n.SS.PluginOpts); opts != nil {
					p["plugin-opts"] = opts
				}
			}
		}

	case "hysteria2":
		if n.Hysteria2 == nil {
			return nil, fmt.Errorf("Hysteria2 配置为空")
		}
		h := n.Hysteria2
		p["type"] = "hysteria2"
		if h.Password != "" {
			p["password"] = h.Password
		}
		if h.SNI != "" {
			p["sni"] = h.SNI
		}
		if h.Insecure {
			p["skip-cert-verify"] = true
		}
		if len(h.ALPN) > 0 {
			p["alpn"] = h.ALPN
		}
		if h.UpMbps > 0 {
			p["up"] = h.UpMbps
		}
		if h.DownMbps > 0 {
			p["down"] = h.DownMbps
		}
		if h.Obfs != "" {
			p["obfs"] = h.Obfs
			if h.ObfsPassword != "" {
				p["obfs-password"] = h.ObfsPassword
			}
		}

	case "hysteria":
		if n.Hysteria == nil {
			return nil, fmt.Errorf("Hysteria 配置为空")
		}
		h := n.Hysteria
		p["type"] = "hysteria"
		if h.AuthStr != "" {
			p["auth-str"] = h.AuthStr
		}
		if h.SNI != "" {
			p["sni"] = h.SNI
		}
		if h.Insecure {
			p["skip-cert-verify"] = true
		}
		if len(h.ALPN) > 0 {
			p["alpn"] = h.ALPN
		}
		if h.UpMbps > 0 {
			p["up"] = h.UpMbps
		}
		if h.DownMbps > 0 {
			p["down"] = h.DownMbps
		}
		if h.Obfs != "" {
			p["obfs"] = h.Obfs
		}

	case "tuic":
		if n.TUIC == nil {
			return nil, fmt.Errorf("TUIC 配置为空")
		}
		t := n.TUIC
		p["type"] = "tuic"
		p["uuid"] = t.UUID
		if t.Password != "" {
			p["password"] = t.Password
		}
		if t.SNI != "" {
			p["sni"] = t.SNI
		}
		if len(t.ALPN) > 0 {
			p["alpn"] = t.ALPN
		}
		p["congestion-controller"] = orDefault(t.CongestionControl, "cubic")
		if t.UDPRelayMode != "" {
			p["udp-relay-mode"] = t.UDPRelayMode
		}
		if t.Insecure {
			p["skip-cert-verify"] = true
		}

	case "socks":
		if n.Socks == nil {
			return nil, fmt.Errorf("Socks 配置为空")
		}
		p["type"] = "socks5"
		if n.Socks.Username != "" {
			p["username"] = n.Socks.Username
		}
		if n.Socks.Password != "" {
			p["password"] = n.Socks.Password
		}

	case "http":
		if n.HTTP == nil {
			return nil, fmt.Errorf("HTTP 配置为空")
		}
		p["type"] = "http"
		if n.HTTP.Username != "" {
			p["username"] = n.HTTP.Username
		}
		if n.HTTP.Password != "" {
			p["password"] = n.HTTP.Password
		}
		if n.HTTP.TLS {
			p["tls"] = true
		}
		if n.HTTP.SNI != "" {
			p["sni"] = n.HTTP.SNI
		}
		if n.HTTP.Insecure {
			p["skip-cert-verify"] = true
		}
		if len(n.HTTP.ALPN) > 0 {
			p["alpn"] = n.HTTP.ALPN
		}

	case "anytls":
		if n.AnyTLS == nil {
			return nil, fmt.Errorf("AnyTLS 配置为空")
		}
		p["type"] = "anytls"
		p["password"] = n.AnyTLS.Password
		if n.AnyTLS.SNI != "" {
			p["sni"] = n.AnyTLS.SNI
		}
		if n.AnyTLS.Insecure {
			p["skip-cert-verify"] = true
		}
		if len(n.AnyTLS.ALPN) > 0 {
			p["alpn"] = n.AnyTLS.ALPN
		}
		if n.AnyTLS.Fingerprint != "" {
			p["client-fingerprint"] = n.AnyTLS.Fingerprint
		}

	case "ssr":
		if n.SSR == nil {
			return nil, fmt.Errorf("SSR 配置为空")
		}
		p["type"] = "ssr"
		if n.SSR.Method != "" {
			p["cipher"] = n.SSR.Method
		}
		p["password"] = n.SSR.Password
		if n.SSR.Protocol != "" {
			p["protocol"] = n.SSR.Protocol
		}
		if n.SSR.ProtocolParam != "" {
			p["protocol-param"] = n.SSR.ProtocolParam
		}
		if n.SSR.Obfs != "" {
			p["obfs"] = n.SSR.Obfs
		}
		if n.SSR.ObfsParam != "" {
			p["obfs-param"] = n.SSR.ObfsParam
		}

	case "wireguard":
		if n.WireGuard == nil {
			return nil, fmt.Errorf("WireGuard 配置为空")
		}
		wg := n.WireGuard
		p["type"] = "wireguard"
		p["private-key"] = wg.PrivateKey
		p["public-key"] = wg.PublicKey
		if wg.PresharedKey != "" {
			p["pre-shared-key"] = wg.PresharedKey
		}
		if len(wg.Reserved) > 0 {
			p["reserved"] = wg.Reserved
		}
		if len(wg.LocalAddress) > 0 {
			p["ip"] = wg.LocalAddress[0]
			if len(wg.LocalAddress) > 1 {
				p["ipv6"] = wg.LocalAddress[1]
			}
		}
		if wg.MTU > 0 {
			p["mtu"] = wg.MTU
		}
		if len(wg.DNS) > 0 {
			p["dns"] = wg.DNS
		}

	default:
		// ssh 等仅有 RawOutbound 的节点无法表达为 Clash 条目
		return nil, fmt.Errorf("协议 %s 不支持写入 mihomo 配置", n.Protocol)
	}
	return p, nil
}

// setClashTLS 写入 Clash 通用 TLS 字段（vmess/vless 使用 servername）。
func setClashTLS(p map[string]interface{}, tls bool, sni string, alpn []string, insecure bool, fingerprint string) {
	if tls {
		p["tls"] = true
	}
	if sni != "" {
		p["servername"] = sni
	}
	if len(alpn) > 0 {
		p["alpn"] = alpn
	}
	if insecure {
		p["skip-cert-verify"] = true
	}
	if fingerprint != "" {
		p["client-fingerprint"] = fingerprint
	}
}

// applyClashTransport 把 TransportConfig 写回 Clash 的 network + "*-opts" 结构。
func applyClashTransport(p map[string]interface{}, t *TransportConfig) {
	if t == nil || t.Type == "" {
		return
	}
	p["network"] = t.Type
	opts := map[string]interface{}{}
	switch t.Type {
	case "ws":
		if t.Path != "" {
			opts["path"] = t.Path
		}
		if t.Host != "" {
			opts["headers"] = map[string]interface{}{"Host": t.Host}
		}
		if t.MaxEarlyData > 0 {
			opts["max-early-data"] = t.MaxEarlyData
			opts["early-data-header-name"] = orDefault(t.EarlyDataHeaderName, "Sec-WebSocket-Protocol")
		}
	case "http":
		if t.Path != "" {
			opts["path"] = t.Path
		}
		if t.Host != "" {
			opts["host"] = []interface{}{t.Host}
		}
	case "grpc":
		if t.ServiceName != "" {
			opts["grpc-service-name"] = t.ServiceName
		}
	case "httpupgrade":
		if t.Path != "" {
			opts["path"] = t.Path
		}
		if t.Host != "" {
			opts["host"] = t.Host
		}
	case "xhttp":
		if t.Path != "" {
			opts["path"] = t.Path
		}
		if t.Host != "" {
			opts["host"] = []interface{}{t.Host}
		}
		if t.Mode != "" {
			opts["mode"] = t.Mode
		}
	case "quic":
		// 无附加字段
	default:
		return // 未知 transport 不写 network，避免产生非法配置
	}
	if len(opts) > 0 {
		key := t.Type + "-opts"
		if t.Type == "http" {
			key = "h2-opts"
		}
		p[key] = opts
	}
}

// parsePluginOpts 把 sing-box 风格的 "k=v;k=v" 插件参数串转回 Clash 的 map。
func parsePluginOpts(s string) map[string]interface{} {
	m := map[string]interface{}{}
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// cloneClashProxy 深拷贝一个 Clash proxies 条目（经 yaml 序列化往返，
// 同时把类型规范化为 yaml.v3 的原生类型，保证写入/回读后逐字段可比较）。
func cloneClashProxy(m map[string]interface{}) map[string]interface{} {
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
