package node

// Node represents a proxy node parsed from a URI or subscription.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // vmess | vless | trojan | ss | hysteria | hysteria2 | tuic | socks | http | anytls | ssr | wireguard | ...
	Address  string `json:"address"`
	Port     int    `json:"port"`
	SubURL   string `json:"sub_url,omitempty"`
	GroupID  string `json:"group_id,omitempty"` // 所属分组 ID, 空 = 默认分组

	// RawOutbound stores the original sing-box outbound JSON object.
	// Following v2rayN's approach: any sing-box outbound protocol (ssh,
	// shadowtls, shadowsocksr, mieru, tor, future protocols...) round-trips
	// losslessly — every TLS type (standard / uTLS / Reality / ECH) and every
	// transport field is preserved verbatim and re-applied as-is.
	RawOutbound map[string]interface{} `json:"raw_outbound,omitempty"`

	// RawClashProxy stores the original Clash/mihomo proxies entry (YAML map)
	// for nodes imported from Clash YAML, enabling lossless re-apply when
	// writing mihomo configs.
	RawClashProxy map[string]interface{} `json:"raw_clash_proxy,omitempty"`

	VMess     *VMessConfig      `json:"vmess,omitempty"`
	VLESS     *VLESSConfig      `json:"vless,omitempty"`
	Trojan    *TrojanConfig     `json:"trojan,omitempty"`
	SS        *SSConfig         `json:"ss,omitempty"`
	Hysteria  *HysteriaConfig   `json:"hysteria,omitempty"`
	Hysteria2 *Hysteria2Config  `json:"hysteria2,omitempty"`
	TUIC      *TUICConfig       `json:"tuic,omitempty"`
	Socks     *SocksConfig      `json:"socks,omitempty"`
	HTTP      *HTTPConfig       `json:"http,omitempty"`
	AnyTLS    *AnyTLSConfig     `json:"anytls,omitempty"`
	SSR       *SSRConfig        `json:"ssr,omitempty"`
	WireGuard *WireGuardConfig  `json:"wireguard,omitempty"`
}

// TransportConfig holds V2Ray transport settings shared by VMess/VLESS/Trojan.
// sing-box transport types (complete list): ws | http | grpc | httpupgrade | quic
// xhttp (Xray transport, supported by sing-box forks) is also accepted and
// written following the Xray spec: path + host[] + mode.
// URI "type" / vmess-json "net" values map to these as follows:
//
//	ws          → ws
//	h2 / http   → http  (h2 is the URI alias, sing-box uses "http")
//	grpc        → grpc
//	httpupgrade → httpupgrade
//	quic        → quic
//	xhttp / splithttp → xhttp (Xray; ws-like path/host style in URI)
//	tcp / raw / "" → (no transport block)
//	kcp → NOT supported by sing-box (rejected)
type TransportConfig struct {
	Type string `json:"type"` // ws | http | grpc | httpupgrade | quic | xhttp

	// ws / http / httpupgrade
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"` // for ws: goes into headers["Host"]; for http/httpupgrade: top-level "host"

	// ws only
	MaxEarlyData        int    `json:"max_early_data,omitempty"`         // ws early data size (Xray: earlyData / ed)
	EarlyDataHeaderName string `json:"early_data_header_name,omitempty"` // usually "Sec-WebSocket-Protocol"

	// grpc only
	ServiceName string `json:"service_name,omitempty"` // URI: serviceName / path

	// xhttp only (Xray transport)
	Mode string `json:"mode,omitempty"` // auto(default) | packet-up | stream-up | stream-one
}

// ── VMess ─────────────────────────────────────────────────────────────────────
// sing-box fields: uuid(req), security, alter_id, tls, transport
// security: auto(default) | none | zero | aes-128-gcm | chacha20-poly1305
type VMessConfig struct {
	UUID     string           `json:"uuid"`
	AlterID  int              `json:"alter_id"` // 0=AEAD (recommended), ≥1=legacy
	Security string           `json:"security"` // default "auto"; must NOT be empty string
	TLS      bool             `json:"tls"`
	SNI      string           `json:"sni,omitempty"`
	ALPN     []string         `json:"alpn,omitempty"`
	Fingerprint string        `json:"fingerprint,omitempty"` // uTLS fingerprint (URI: fp / vmess-json: fp)
	Insecure    bool          `json:"insecure,omitempty"`    // vmess-json: verify_cert=false / allowInsecure=1
	ECHConfig   string        `json:"ech_config,omitempty"`  // TLS ECH config list (URI: ech)
	Transport *TransportConfig `json:"transport,omitempty"`
}

// ── VLESS ─────────────────────────────────────────────────────────────────────
// sing-box fields: uuid(req), flow, tls, transport
// flow: "" | "xtls-rprx-vision"
type VLESSConfig struct {
	UUID        string           `json:"uuid"`
	Flow        string           `json:"flow,omitempty"`        // "xtls-rprx-vision" or ""
	TLS         bool             `json:"tls"`
	SNI         string           `json:"sni,omitempty"`
	ALPN        []string         `json:"alpn,omitempty"`
	Fingerprint string           `json:"fingerprint,omitempty"` // uTLS fingerprint (URI: fp)
	Insecure    bool             `json:"insecure,omitempty"`    // URI: insecure / allowInsecure
	ECHConfig   string           `json:"ech_config,omitempty"`  // TLS ECH config list (URI: ech)
	// Reality fields (TLS must be true)
	PublicKey string `json:"public_key,omitempty"` // URI: pbk
	ShortID   string `json:"short_id,omitempty"`   // URI: sid
	Transport *TransportConfig `json:"transport,omitempty"`
}

// ── Trojan ────────────────────────────────────────────────────────────────────
// sing-box fields: password(req), tls, transport
type TrojanConfig struct {
	Password string           `json:"password"`
	SNI      string           `json:"sni,omitempty"`
	ALPN     []string         `json:"alpn,omitempty"`
	Fingerprint string        `json:"fingerprint,omitempty"` // URI: fp
	Insecure    bool          `json:"insecure,omitempty"`    // URI: insecure / allowInsecure
	ECHConfig   string        `json:"ech_config,omitempty"`  // URI: ech
	Transport *TransportConfig `json:"transport,omitempty"`
}

// ── Shadowsocks ───────────────────────────────────────────────────────────────
// sing-box fields: method(req), password(req), plugin, plugin_opts
// Common methods: 2022-blake3-aes-128-gcm | 2022-blake3-aes-256-gcm |
//   2022-blake3-chacha20-poly1305 | aes-128-gcm | aes-256-gcm |
//   chacha20-ietf-poly1305 | xchacha20-ietf-poly1305 | none
type SSConfig struct {
	Method     string `json:"method"`
	Password   string `json:"password"`
	Plugin     string `json:"plugin,omitempty"`      // obfs-local | v2ray-plugin
	PluginOpts string `json:"plugin_opts,omitempty"` // SIP003 plugin options, e.g. "obfs=http;obfs-host=xxx"
}

// ── Hysteria (v1, deprecated in sing-box 1.12+) ───────────────────────────────
// sing-box fields: up_mbps, down_mbps, obfs, auth_str, tls(Required)
type HysteriaConfig struct {
	AuthStr  string   `json:"auth_str,omitempty"`
	SNI      string   `json:"sni,omitempty"`
	Insecure bool     `json:"insecure,omitempty"`
	ALPN     []string `json:"alpn,omitempty"`
	UpMbps   int      `json:"up_mbps,omitempty"`
	DownMbps int      `json:"down_mbps,omitempty"`
	Obfs     string   `json:"obfs,omitempty"` // string obfs password (hysteria v1)
}

// ── Hysteria2 ─────────────────────────────────────────────────────────────────
// sing-box fields: password, up_mbps, down_mbps, obfs.{type,password}, tls(Required)
// obfs.type: "salamander" (official) | "gecko" (newer sing-box)
type Hysteria2Config struct {
	Password     string   `json:"password"`
	SNI          string   `json:"sni,omitempty"`
	Insecure     bool     `json:"insecure,omitempty"`
	ALPN         []string `json:"alpn,omitempty"`
	ECHConfig    string   `json:"ech_config,omitempty"` // URI: ech
	UpMbps       int      `json:"up_mbps,omitempty"`    // 0 = BBR CC (no limit)
	DownMbps     int      `json:"down_mbps,omitempty"`  // 0 = BBR CC (no limit)
	Obfs         string   `json:"obfs,omitempty"`       // "salamander"
	ObfsPassword string   `json:"obfs_password,omitempty"`
}

// ── TUIC ──────────────────────────────────────────────────────────────────────
// sing-box fields: uuid(req), password, congestion_control, udp_relay_mode, tls(Required)
// congestion_control: cubic(default) | new_reno | bbr
// udp_relay_mode: native(default) | quic
type TUICConfig struct {
	UUID              string   `json:"uuid"`
	Password          string   `json:"password,omitempty"`
	SNI               string   `json:"sni,omitempty"`
	ALPN              []string `json:"alpn,omitempty"`
	Insecure          bool     `json:"insecure,omitempty"`
	CongestionControl string   `json:"congestion_control,omitempty"` // default "cubic"
	UDPRelayMode      string   `json:"udp_relay_mode,omitempty"`     // default "native"
}

// ── Socks ─────────────────────────────────────────────────────────────────────
// sing-box fields: version("5"), server, server_port, username, password
type SocksConfig struct {
	Version  string `json:"version"` // "5" (default) | "4a"
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// ── HTTP(S) proxy ─────────────────────────────────────────────────────────────
// sing-box fields: server, server_port, username, password, tls
type HTTPConfig struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TLS      bool   `json:"tls,omitempty"`
	SNI      string `json:"sni,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	ALPN     []string `json:"alpn,omitempty"`
}

// ── AnyTLS ────────────────────────────────────────────────────────────────────
// sing-box fields: password(req), tls(Required), idle_session_check_interval...
type AnyTLSConfig struct {
	Password    string   `json:"password"`
	SNI         string   `json:"sni,omitempty"`
	Insecure    bool     `json:"insecure,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"` // uTLS fingerprint (URI: fp)
	ECHConfig   string   `json:"ech_config,omitempty"`  // URI: ech
}

// ── ShadowsocksR (removed from sing-box 1.13, kept for older cores) ───────────
// Applied as a raw outbound {type: "shadowsocksr", ...}
type SSRConfig struct {
	Method        string `json:"method"`
	Password      string `json:"password"`
	Protocol      string `json:"protocol,omitempty"`       // origin | auth_sha1_v4 | ...
	ProtocolParam string `json:"protocol_param,omitempty"`
	Obfs          string `json:"obfs,omitempty"`           // plain | http_simple | tls1.2_ticket_auth...
	ObfsParam     string `json:"obfs_param,omitempty"`
}

// ── WireGuard (sing-box 1.13+: endpoint; outbound still supported) ────────────
type WireGuardConfig struct {
	PrivateKey   string   `json:"private_key"`            // URI userinfo (secret key)
	PublicKey    string   `json:"peer_public_key"`        // URI: publickey
	PresharedKey string   `json:"pre_shared_key,omitempty"` // URI: presharedkey
	Reserved     []int    `json:"reserved,omitempty"`     // URI: reserved "1,2,3" or base64
	LocalAddress []string `json:"local_address,omitempty"` // URI: address
	MTU          int      `json:"mtu,omitempty"`          // URI: mtu
	DNS          []string `json:"dns,omitempty"`          // URI: dns
}
