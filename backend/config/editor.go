package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"sm-gui/backend/node"
)

func loadJSON(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}
	return cfg, nil
}

func saveJSON(path string, cfg map[string]interface{}) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
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

func getInbounds(cfg map[string]interface{}) []interface{} {
	v, _ := cfg["inbounds"].([]interface{})
	return v
}

func getOutbounds(cfg map[string]interface{}) []interface{} {
	v, _ := cfg["outbounds"].([]interface{})
	return v
}

// ─── Apply Node ───────────────────────────────────────────────────────────────

// ApplyNodeToConfig 按内核把节点写入配置文件。
// sing-box：替换 tag 为 "proxy" 的 outbound；mihomo：替换 name 为 "proxy" 的 proxies 条目。
func ApplyNodeToConfig(core, cfgPath string, n node.Node) error {
	if core == CoreMihomo {
		return ApplyNodeToMihomoConfig(cfgPath, n)
	}
	return applyNodeToSingBoxConfig(cfgPath, n)
}

// applyNodeToSingBoxConfig replaces the "proxy" outbound in a sing-box JSON config.
func applyNodeToSingBoxConfig(cfgPath string, n node.Node) error {
	cfg, err := loadJSON(cfgPath)
	if err != nil {
		return err
	}
	var outbound map[string]interface{}
	if n.RawOutbound != nil {
		// Verbatim re-apply of the original sing-box outbound JSON (v2rayN approach).
		// Covers ALL outbound protocols and ALL TLS types (standard/uTLS/Reality/ECH)
		// with any field combination, without lossy conversion.
		outbound = n.RawOutbound
	} else {
		outbound, err = nodeToSingBoxOutbound(n)
		if err != nil {
			return err
		}
	}
	outbounds := getOutbounds(cfg)
	replaced := false
	for i, ob := range outbounds {
		if m, ok := ob.(map[string]interface{}); ok {
			if m["tag"] == "proxy" {
				outbound["tag"] = "proxy"
				outbounds[i] = outbound
				replaced = true
				break
			}
		}
	}
	if !replaced {
		outbound["tag"] = "proxy"
		outbounds = append(outbounds, outbound)
	}
	cfg["outbounds"] = outbounds
	return saveJSON(cfgPath, cfg)
}

// nodeToSingBoxOutbound converts a Node to a sing-box outbound map.
// All field names match the official sing-box documentation exactly.
func nodeToSingBoxOutbound(n node.Node) (map[string]interface{}, error) {
	ob := map[string]interface{}{
		"tag":         "proxy",
		"server":      n.Address,
		"server_port": n.Port,
	}

	switch n.Protocol {

	// ── VMess ─────────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/vmess/
	case "vmess":
		if n.VMess == nil {
			return nil, fmt.Errorf("VMess 配置为空")
		}
		ob["type"] = "vmess"
		ob["uuid"] = n.VMess.UUID
		ob["alter_id"] = n.VMess.AlterID
		// security must NOT be empty string — default to "auto"
		ob["security"] = orDefault(n.VMess.Security, "auto")
		if t := buildTransport(n.VMess.Transport); t != nil {
			ob["transport"] = t
		}
		if n.VMess.TLS {
			ob["tls"] = buildTLS(n.VMess.SNI, n.VMess.ALPN, n.VMess.Insecure, n.VMess.Fingerprint, n.VMess.ECHConfig)
		}

	// ── VLESS ─────────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/vless/
	case "vless":
		if n.VLESS == nil {
			return nil, fmt.Errorf("VLESS 配置为空")
		}
		ob["type"] = "vless"
		ob["uuid"] = n.VLESS.UUID
		if n.VLESS.Flow != "" {
			ob["flow"] = n.VLESS.Flow
		}
		if t := buildTransport(n.VLESS.Transport); t != nil {
			ob["transport"] = t
		}
		if n.VLESS.TLS {
			if n.VLESS.PublicKey != "" {
				tls := buildRealityTLS(n.VLESS.SNI, n.VLESS.PublicKey, n.VLESS.ShortID, n.VLESS.Fingerprint)
				if n.VLESS.Insecure {
					tls["insecure"] = true
				}
				ob["tls"] = tls
			} else {
				ob["tls"] = buildTLS(n.VLESS.SNI, n.VLESS.ALPN, n.VLESS.Insecure, n.VLESS.Fingerprint, n.VLESS.ECHConfig)
			}
		}

	// ── Trojan ────────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/trojan/
	case "trojan":
		if n.Trojan == nil {
			return nil, fmt.Errorf("Trojan 配置为空")
		}
		ob["type"] = "trojan"
		ob["password"] = n.Trojan.Password
		if t := buildTransport(n.Trojan.Transport); t != nil {
			ob["transport"] = t
		}
		// Trojan always uses TLS
		ob["tls"] = buildTLS(n.Trojan.SNI, n.Trojan.ALPN, n.Trojan.Insecure, n.Trojan.Fingerprint, n.Trojan.ECHConfig)

	// ── Shadowsocks ───────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/shadowsocks/
	case "ss":
		if n.SS == nil {
			return nil, fmt.Errorf("Shadowsocks 配置为空")
		}
		ob["type"] = "shadowsocks"
		ob["method"] = n.SS.Method
		ob["password"] = n.SS.Password
		if n.SS.Plugin != "" {
			ob["plugin"] = n.SS.Plugin
			if n.SS.PluginOpts != "" {
				ob["plugin_opts"] = n.SS.PluginOpts
			}
		}

	// ── Hysteria (v1) ─────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/hysteria/
	// (deprecated since sing-box 1.12 — tls is Required)
	case "hysteria":
		if n.Hysteria == nil {
			return nil, fmt.Errorf("Hysteria 配置为空")
		}
		ob["type"] = "hysteria"
		if n.Hysteria.AuthStr != "" {
			ob["auth_str"] = n.Hysteria.AuthStr
		}
		if n.Hysteria.UpMbps > 0 {
			ob["up_mbps"] = n.Hysteria.UpMbps
		}
		if n.Hysteria.DownMbps > 0 {
			ob["down_mbps"] = n.Hysteria.DownMbps
		}
		if n.Hysteria.Obfs != "" {
			ob["obfs"] = n.Hysteria.Obfs
		}
		tls := map[string]interface{}{"enabled": true}
		if n.Hysteria.SNI != "" {
			tls["server_name"] = n.Hysteria.SNI
		}
		if n.Hysteria.Insecure {
			tls["insecure"] = true
		}
		if len(n.Hysteria.ALPN) > 0 {
			tls["alpn"] = n.Hysteria.ALPN
		}
		ob["tls"] = tls

	// ── Hysteria2 ─────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/hysteria2/
	// tls is ==Required==
	case "hysteria2":
		if n.Hysteria2 == nil {
			return nil, fmt.Errorf("Hysteria2 配置为空")
		}
		ob["type"] = "hysteria2"
		if n.Hysteria2.Password != "" {
			ob["password"] = n.Hysteria2.Password
		}
		if n.Hysteria2.UpMbps > 0 {
			ob["up_mbps"] = n.Hysteria2.UpMbps
		}
		if n.Hysteria2.DownMbps > 0 {
			ob["down_mbps"] = n.Hysteria2.DownMbps
		}
		if n.Hysteria2.Obfs != "" {
			obfs := map[string]interface{}{"type": n.Hysteria2.Obfs}
			if n.Hysteria2.ObfsPassword != "" {
				obfs["password"] = n.Hysteria2.ObfsPassword
			}
			ob["obfs"] = obfs
		}
		// tls Required — always include with enabled:true
		tls := map[string]interface{}{"enabled": true}
		if n.Hysteria2.SNI != "" {
			tls["server_name"] = n.Hysteria2.SNI
		}
		if n.Hysteria2.Insecure {
			tls["insecure"] = true
		}
		if len(n.Hysteria2.ALPN) > 0 {
			tls["alpn"] = n.Hysteria2.ALPN
		}
		if n.Hysteria2.ECHConfig != "" {
			tls["ech"] = map[string]interface{}{
				"enabled": true,
				"config":  n.Hysteria2.ECHConfig,
			}
		}
		ob["tls"] = tls

	// ── TUIC ──────────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/tuic/
	// tls is ==Required==
	case "tuic":
		if n.TUIC == nil {
			return nil, fmt.Errorf("TUIC 配置为空")
		}
		ob["type"] = "tuic"
		ob["uuid"] = n.TUIC.UUID
		if n.TUIC.Password != "" {
			ob["password"] = n.TUIC.Password
		}
		// congestion_control: cubic(default) | new_reno | bbr
		ob["congestion_control"] = orDefault(n.TUIC.CongestionControl, "cubic")
		// udp_relay_mode: native(default) | quic — omit to use default
		if n.TUIC.UDPRelayMode != "" {
			ob["udp_relay_mode"] = n.TUIC.UDPRelayMode
		}
		// tls Required — always include with enabled:true
		tls := map[string]interface{}{"enabled": true}
		if n.TUIC.SNI != "" {
			tls["server_name"] = n.TUIC.SNI
		}
		if n.TUIC.Insecure {
			tls["insecure"] = true
		}
		if len(n.TUIC.ALPN) > 0 {
			tls["alpn"] = n.TUIC.ALPN
		}
		ob["tls"] = tls

	// ── Socks ─────────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/socks/
	case "socks":
		if n.Socks == nil {
			return nil, fmt.Errorf("Socks 配置为空")
		}
		ob["type"] = "socks"
		ob["version"] = orDefault(n.Socks.Version, "5")
		if n.Socks.Username != "" {
			ob["username"] = n.Socks.Username
		}
		if n.Socks.Password != "" {
			ob["password"] = n.Socks.Password
		}

	// ── HTTP(S) ───────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/http/
	case "http":
		if n.HTTP == nil {
			return nil, fmt.Errorf("HTTP 配置为空")
		}
		ob["type"] = "http"
		if n.HTTP.Username != "" {
			ob["username"] = n.HTTP.Username
		}
		if n.HTTP.Password != "" {
			ob["password"] = n.HTTP.Password
		}
		if n.HTTP.TLS {
			ob["tls"] = buildTLS(n.HTTP.SNI, n.HTTP.ALPN, n.HTTP.Insecure, "", "")
		}

	// ── AnyTLS ────────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/anytls/
	// tls is ==Required==
	case "anytls":
		if n.AnyTLS == nil {
			return nil, fmt.Errorf("AnyTLS 配置为空")
		}
		ob["type"] = "anytls"
		ob["password"] = n.AnyTLS.Password
		tls := map[string]interface{}{"enabled": true}
		if n.AnyTLS.SNI != "" {
			tls["server_name"] = n.AnyTLS.SNI
		}
		if n.AnyTLS.Insecure {
			tls["insecure"] = true
		}
		if len(n.AnyTLS.ALPN) > 0 {
			tls["alpn"] = n.AnyTLS.ALPN
		}
		if n.AnyTLS.Fingerprint != "" {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": n.AnyTLS.Fingerprint,
			}
		}
		if n.AnyTLS.ECHConfig != "" {
			tls["ech"] = map[string]interface{}{
				"enabled": true,
				"config":  n.AnyTLS.ECHConfig,
			}
		}
		ob["tls"] = tls

	// ── ShadowsocksR ──────────────────────────────────────────────────────────
	// Removed from sing-box 1.13; still supported by older cores.
	case "ssr":
		if n.SSR == nil {
			return nil, fmt.Errorf("SSR 配置为空")
		}
		ob["type"] = "shadowsocksr"
		ob["method"] = n.SSR.Method
		ob["password"] = n.SSR.Password
		if n.SSR.Protocol != "" {
			ob["protocol"] = n.SSR.Protocol
		}
		if n.SSR.ProtocolParam != "" {
			ob["protocol_param"] = n.SSR.ProtocolParam
		}
		if n.SSR.Obfs != "" {
			ob["obfs"] = n.SSR.Obfs
		}
		if n.SSR.ObfsParam != "" {
			ob["obfs_param"] = n.SSR.ObfsParam
		}

	// ── WireGuard ─────────────────────────────────────────────────────────────
	// Docs: https://sing-box.sagernet.org/configuration/outbound/wireguard/
	// (sing-box 1.13+: prefer endpoint form; outbound remains compatible)
	case "wireguard":
		if n.WireGuard == nil {
			return nil, fmt.Errorf("WireGuard 配置为空")
		}
		wg := n.WireGuard
		ob["type"] = "wireguard"
		ob["private_key"] = wg.PrivateKey
		ob["peer_public_key"] = wg.PublicKey
		if wg.PresharedKey != "" {
			ob["pre_shared_key"] = wg.PresharedKey
		}
		if len(wg.LocalAddress) > 0 {
			ob["local_address"] = wg.LocalAddress
		} else {
			ob["local_address"] = []string{"172.16.0.2/32"}
		}
		if len(wg.Reserved) == 3 {
			ob["reserved"] = wg.Reserved
		}
		if wg.MTU > 0 {
			ob["mtu"] = wg.MTU
		}
		if len(wg.DNS) > 0 {
			ob["dns"] = wg.DNS
		}

	default:
		return nil, fmt.Errorf("不支持的协议: %s", n.Protocol)
	}
	return ob, nil
}

// ─── Transport builder ────────────────────────────────────────────────────────
// Converts our TransportConfig into the sing-box "transport" object.
//
// sing-box transport field reference (complete list):
//
//   ws:
//     type, path, headers{}, max_early_data, early_data_header_name
//     NOTE: Host goes inside headers["Host"], NOT a top-level "host" field.
//
//   http (h2/h3):
//     type, host[], path, method, headers{}, idle_timeout, ping_timeout
//     NOTE: host is a []string array. path is a plain string.
//     With tls.alpn=["h3"] the transport uses HTTP/3 instead of HTTP/2.
//
//   grpc:
//     type, service_name, idle_timeout, ping_timeout, permit_without_stream
//
//   httpupgrade:
//     type, host, path, headers{}
//     NOTE: host is a TOP-LEVEL string field, NOT inside headers.
//     (See sing-box issue #1841 — putting host in headers["Host"] does NOT work)
//
//   quic:
//     type  (no other user-facing fields)
//
//   xhttp (Xray transport, for sing-box forks that support it):
//     type, path, host[], mode (auto/packet-up/stream-up/stream-one)
//     Written following the Xray spec.

func buildTransport(t *node.TransportConfig) map[string]interface{} {
	if t == nil || t.Type == "" {
		return nil
	}
	m := map[string]interface{}{"type": t.Type}

	switch t.Type {
	case "ws":
		if t.Path != "" {
			m["path"] = t.Path
		}
		// Host goes into headers["Host"] for WebSocket
		if t.Host != "" {
			m["headers"] = map[string]interface{}{"Host": t.Host}
		}
		// Early data support
		if t.MaxEarlyData > 0 {
			m["max_early_data"] = t.MaxEarlyData
			m["early_data_header_name"] = orDefault(t.EarlyDataHeaderName, "Sec-WebSocket-Protocol")
		}

	case "http":
		// path: plain string (NOT an array)
		if t.Path != "" {
			m["path"] = t.Path
		}
		// host: []string array
		if t.Host != "" {
			m["host"] = []string{t.Host}
		}

	case "grpc":
		if t.ServiceName != "" {
			m["service_name"] = t.ServiceName
		}

	case "httpupgrade":
		if t.Path != "" {
			m["path"] = t.Path
		}
		// host: TOP-LEVEL string field (NOT headers["Host"] — that is the WebSocket behavior)
		if t.Host != "" {
			m["host"] = t.Host
		}

	case "quic":
		// no user-facing fields

	case "xhttp":
		// Xray xhttp transport (sing-box forks). Follows the Xray spec:
		// path is a plain string, host is a []string array, mode selects
		// packet-up / stream-up / stream-one / auto.
		if t.Path != "" {
			m["path"] = t.Path
		}
		if t.Host != "" {
			m["host"] = []string{t.Host}
		}
		if t.Mode != "" {
			m["mode"] = t.Mode
		}

	}
	return m
}

// ─── TLS builders ─────────────────────────────────────────────────────────────
// sing-box outbound TLS fields (complete list):
//   enabled(req), server_name, insecure, alpn[], min_version, max_version,
//   certificate, certificate_public_key_sha256,
//   utls.{enabled, fingerprint},
//   reality.{enabled, public_key(req), short_id(req)},
//   ech.{enabled, config}

func buildTLS(sni string, alpn []string, insecure bool, fingerprint, echConfig string) map[string]interface{} {
	tls := map[string]interface{}{"enabled": true}
	if sni != "" {
		tls["server_name"] = sni
	}
	if insecure {
		tls["insecure"] = true
	}
	if len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	// uTLS fingerprint for browser impersonation
	if fingerprint != "" {
		tls["utls"] = map[string]interface{}{
			"enabled":     true,
			"fingerprint": fingerprint,
		}
	}
	// Encrypted Client Hello
	if echConfig != "" {
		tls["ech"] = map[string]interface{}{
			"enabled": true,
			"config":  echConfig,
		}
	}
	return tls
}

// buildRealityTLS builds the TLS block for VLESS+Reality.
// Reality requires: public_key, short_id, and a uTLS fingerprint (default "chrome").
func buildRealityTLS(sni, publicKey, shortID, fingerprint string) map[string]interface{} {
	tls := map[string]interface{}{
		"enabled": true,
		"reality": map[string]interface{}{
			"enabled":    true,
			"public_key": publicKey,
			"short_id":   shortID,
		},
		"utls": map[string]interface{}{
			"enabled":     true,
			"fingerprint": orDefault(fingerprint, "chrome"),
		},
	}
	if sni != "" {
		tls["server_name"] = sni
	}
	return tls
}

// ─── TUN inbound ─────────────────────────────────────────────────────────────

// SetTun 按内核开关 TUN。mihomo 无 strict_route，该参数对 mihomo 被忽略。
func SetTun(core, cfgPath string, enable bool, stack string, mtu int, strictRoute bool) error {
	if core == CoreMihomo {
		return SetTunMihomo(cfgPath, enable, stack, mtu)
	}
	return setTunSingBox(cfgPath, enable, stack, mtu, strictRoute)
}

// buildTunInbound 根据用户设置构建 tun inbound。
// address 与 interface_name 保持固定（修改会导致路由残留风险，不暴露为设置项）。
func buildTunInbound(stack string, mtu int, strictRoute bool) map[string]interface{} {
	if stack != "gvisor" && stack != "system" && stack != "mixed" {
		stack = "gvisor"
	}
	if mtu <= 0 {
		mtu = 9000
	}
	return map[string]interface{}{
		"type":           "tun",
		"tag":            "tun-in",
		"interface_name": "singbox_tun",
		"address":        []string{"172.18.0.1/30"},
		"mtu":            mtu,
		"auto_route":     true,
		"strict_route":   strictRoute,
		"stack":          stack,
	}
}

// setTunSingBox 开关 sing-box 配置中的 tun inbound。
func setTunSingBox(cfgPath string, enable bool, stack string, mtu int, strictRoute bool) error {
	cfg, err := loadJSON(cfgPath)
	if err != nil {
		return err
	}
	inbounds := getInbounds(cfg)
	newInbounds := []interface{}{}
	for _, ib := range inbounds {
		if m, ok := ib.(map[string]interface{}); ok && m["type"] == "tun" {
			continue
		}
		newInbounds = append(newInbounds, ib)
	}
	if enable {
		newInbounds = append(newInbounds, buildTunInbound(stack, mtu, strictRoute))
	}
	cfg["inbounds"] = newInbounds
	return saveJSON(cfgPath, cfg)
}

// ─── Mixed inbound ────────────────────────────────────────────────────────────

func SetMixedInbound(core, cfgPath string, enable bool, listen string, port int) error {
	if core == CoreMihomo {
		return SetMixedInboundMihomo(cfgPath, enable, listen, port)
	}
	return setMixedInboundSingBox(cfgPath, enable, listen, port)
}

// setMixedInboundSingBox 开关 sing-box 配置中的 mixed inbound。
func setMixedInboundSingBox(cfgPath string, enable bool, listen string, port int) error {
	if strings.TrimSpace(listen) == "" {
		listen = "127.0.0.1"
	}
	if port <= 0 {
		port = 2080
	}
	cfg, err := loadJSON(cfgPath)
	if err != nil {
		return err
	}
	inbounds := getInbounds(cfg)
	newInbounds := []interface{}{}
	for _, ib := range inbounds {
		if m, ok := ib.(map[string]interface{}); ok && m["type"] == "mixed" {
			continue
		}
		newInbounds = append(newInbounds, ib)
	}
	if enable {
		newInbounds = append(newInbounds, map[string]interface{}{
			"type":        "mixed",
			"tag":         "mixed-in",
			"listen":      listen,
			"listen_port": port,
		})
	}
	cfg["inbounds"] = newInbounds
	return saveJSON(cfgPath, cfg)
}

// ─── Applied node detection ──────────────────────────────────────────────────

// HasTunInbound 判断配置文件当前是否启用了 TUN（用于切换配置前探测 TUN 状态）。
func HasTunInbound(core, cfgPath string) bool {
	if core == CoreMihomo {
		return HasTunMihomo(cfgPath)
	}
	cfg, err := loadJSON(cfgPath)
	if err != nil {
		return false
	}
	for _, ib := range getInbounds(cfg) {
		if m, ok := ib.(map[string]interface{}); ok && m["type"] == "tun" {
			return true
		}
	}
	return false
}

// FindAppliedNodeID 找出配置文件中当前应用的节点 ID（按内核定位方式：
// sing-box 为 tag "proxy" 的 outbound，mihomo 为 name "proxy" 的 proxies 条目）。
// 找不到匹配返回 ""（例如配置被手工修改过）。
func FindAppliedNodeID(core, cfgPath string, nodes []node.Node) string {
	if core == CoreMihomo {
		return FindAppliedNodeIDMihomo(cfgPath, nodes)
	}
	return findAppliedNodeIDSingBox(cfgPath, nodes)
}

// findAppliedNodeIDSingBox 读取 sing-box JSON 配置，找出当前 "proxy" outbound 对应的节点 ID。
func findAppliedNodeIDSingBox(cfgPath string, nodes []node.Node) string {
	cfg, err := loadJSON(cfgPath)
	if err != nil {
		return ""
	}
	var proxyOb map[string]interface{}
	for _, ob := range getOutbounds(cfg) {
		if m, ok := ob.(map[string]interface{}); ok && m["tag"] == "proxy" {
			proxyOb = m
			break
		}
	}
	if proxyOb == nil {
		return ""
	}
	current, err := json.Marshal(proxyOb) // map 序列化时 key 有序，结果确定
	if err != nil {
		return ""
	}
	for _, n := range nodes {
		var ob map[string]interface{}
		if n.RawOutbound != nil {
			ob = n.RawOutbound
		} else {
			var err error
			ob, err = nodeToSingBoxOutbound(n)
			if err != nil {
				continue
			}
		}
		ob["tag"] = "proxy"
		data, err := json.Marshal(ob)
		if err != nil {
			continue
		}
		if string(data) == string(current) {
			return n.ID
		}
	}
	return ""
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
