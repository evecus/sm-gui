package node

import (
	"encoding/base64"
	"testing"
)

func base64URLEncode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestParseURIProtocols(t *testing.T) {
	cases := []string{
		// vmess (base64 JSON, ws + tls + fp)
		"vmess://eyJ2IjoiMiIsInBzIjoi5rWL6K+VVM0yIiwiYWRkIjoiMS4yLjMuNCIsInBvcnQiOjQ0MywiaWQiOiJhYWFhYWFhYS1iYmJiLWNjY2MtZGRkZC1lZWVlZWVlZWVlZWUiLCJhaWQiOjAsInNjeSI6ImF1dG8iLCJuZXQiOiJ3cyIsInR5cGUiOiIiLCJob3N0IjoiZXhhbXBsZS5jb20iLCJwYXRoIjoiL3BhdGgiLCJ0bHMiOiJ0bHMiLCJzbmkiOiJleGFtcGxlLmNvbSIsImFscG4iOiJodHRwLzEuMSIsImZwIjoiY2hyb21lIn0=",
		// vless + reality + grpc
		"vless://uuid-1234@example.com:443?type=grpc&security=reality&sni=www.apple.com&fp=chrome&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision&serviceName=grpc-svc#VLESS-Reality",
		// vless + ws + tls + insecure
		"vless://uuid-5678@example.com:443?type=ws&security=tls&sni=example.com&path=%2Fws&host=example.com&insecure=1#VLESS-WS",
		// trojan + fp + insecure
		"trojan://pass123@example.com:443?sni=example.com&fp=safari&allowInsecure=1&type=ws&path=%2Ftrojan#Trojan-Node",
		// ss SIP002 with plugin
		"ss://YWVzLTI1Ni1nY206cGFzcw==@1.2.3.4:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com#SS-Node",
		// hysteria2 with up/down/obfs
		"hysteria2://pass%3Aword@example.com:443?sni=example.com&insecure=1&obfs=salamander&obfs-password=obfspass&upmbps=100&downmbps=500#HY2",
		// tuic
		"tuic://uuid-9900:password@example.com:443?sni=example.com&congestion_control=bbr&udp_relay_mode=quic&allow_insecure=1&TUIC-Node",
		// hysteria v1
		"hysteria://1.2.3.4:36712?auth=secret123&peer=example.com&insecure=1&upmbps=50&downmbps=200&obfs=xplus#HY1",
		// socks
		"socks://user:pass@1.2.3.4:1080#SOCKS-Node",
		// anytls
		"anytls://password123@example.com:8443?sni=example.com&insecure=1&fp=chrome#AnyTLS-Node",
		// ssr
		"ssr://MS4yLjMuNDo0NDM6YXV0aF9zaGExX3Y0OmFlcy0yNTYtY2ZiOnRsczEuMl90aWNrZXRfYXV0aDpHdG5CM05uVUEvP29iZnNwYXJhbT0mcHJvdG9wYXJhbT0mcmVtYXJrcz1zc3ItbmlrbmFtZQ==",
		// wireguard
		"wireguard://privKEYbase64@1.2.3.4:51820?publickey=peerKEY&presharedkey=psk&address=10.0.0.2%2F32&reserved=AQID&mtu=1420#WG-Node",
	}
	for _, uri := range cases {
		n, err := ParseURI(uri)
		if err != nil {
			t.Errorf("ParseURI(%s) error: %v", uri[:30], err)
			continue
		}
		t.Logf("OK %-10s %-20s %s:%d", n.Protocol, n.Name, n.Address, n.Port)
	}
}

func TestParseVMessFields(t *testing.T) {
	uri := "vmess://eyJ2IjoiMiIsInBzIjoi5rWL6K+VVM0yIiwiYWRkIjoiMS4yLjMuNCIsInBvcnQiOjQ0MywiaWQiOiJhYWFhYWFhYS1iYmJiLWNjY2MtZGRkZC1lZWVlZWVlZWVlZWUiLCJhaWQiOjAsInNjeSI6ImF1dG8iLCJuZXQiOiJ3cyIsInR5cGUiOiIiLCJob3N0IjoiZXhhbXBsZS5jb20iLCJwYXRoIjoiL3BhdGgiLCJ0bHMiOiJ0bHMiLCJzbmkiOiJleGFtcGxlLmNvbSIsImFscG4iOiJodHRwLzEuMSIsImZwIjoiY2hyb21lIiwidmVyaWZ5X2NlcnQiOmZhbHNlfQ=="
	n, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("vmess parse error: %v", err)
	}
	if n.VMess.Fingerprint != "chrome" {
		t.Errorf("vmess fp = %q, want chrome", n.VMess.Fingerprint)
	}
	if !n.VMess.Insecure {
		t.Errorf("vmess insecure should be true (verify_cert=false)")
	}
	if n.VMess.Transport == nil || n.VMess.Transport.Type != "ws" {
		t.Errorf("vmess transport should be ws")
	}
}

func TestParseSSPlugin(t *testing.T) {
	n, err := ParseURI("ss://YWVzLTI1Ni1nY206cGFzcw==@1.2.3.4:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com#SS")
	if err != nil {
		t.Fatalf("ss parse error: %v", err)
	}
	if n.SS.Plugin != "obfs-local" {
		t.Errorf("ss plugin = %q, want obfs-local", n.SS.Plugin)
	}
	if n.SS.PluginOpts != "obfs=http;obfs-host=example.com" {
		t.Errorf("ss plugin_opts = %q, want obfs=http;obfs-host=example.com", n.SS.PluginOpts)
	}
}

func TestParseHysteria2Fields(t *testing.T) {
	n, err := ParseURI("hysteria2://pass:word@example.com:443?sni=example.com&insecure=1&obfs=salamander&obfs-password=obfspass&upmbps=100&downmbps=500&pinSHA256=ABCD#HY2")
	if err != nil {
		t.Fatalf("hy2 parse error: %v", err)
	}
	if n.Hysteria2.Password != "pass:word" {
		t.Errorf("hy2 password = %q", n.Hysteria2.Password)
	}
	if n.Hysteria2.UpMbps != 100 || n.Hysteria2.DownMbps != 500 {
		t.Errorf("hy2 up/down = %d/%d", n.Hysteria2.UpMbps, n.Hysteria2.DownMbps)
	}
	if !n.Hysteria2.Insecure {
		t.Errorf("hy2 insecure should be true (pinSHA256)")
	}
}

func TestNormalizeNetworkRejectsXrayOnly(t *testing.T) {
	for _, net := range []string{"kcp"} {
		if _, err := normalizeNetwork(net); err == nil {
			t.Errorf("normalizeNetwork(%q) should fail on sing-box", net)
		}
	}
	if v, err := normalizeNetwork("xhttp"); err != nil || v != "xhttp" {
		t.Errorf("xhttp should map to xhttp, got %q err %v", v, err)
	}
	if v, err := normalizeNetwork("raw"); err != nil || v != "" {
		t.Errorf("raw should map to empty transport")
	}
	if v, _ := normalizeNetwork("h2"); v != "http" {
		t.Errorf("h2 should map to http")
	}
}

func TestExportRoundTrip(t *testing.T) {
	// parse → export → parse again, key fields must survive the round trip
	cases := []string{
		"vless://uuid-1234@example.com:443?type=grpc&security=reality&sni=www.apple.com&fp=chrome&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision&serviceName=svc#VLESS",
		"vless://uuid-5678@example.com:443?type=xhttp&security=tls&sni=example.com&path=%2Fxp&host=example.com&mode=stream-up#VLESS-XHTTP",
		"trojan://pass123@example.com:443?sni=example.com&fp=safari&allowInsecure=1&type=ws&path=%2Ftrojan#Trojan",
		"ss://YWVzLTI1Ni1nY206cGFzcw==@1.2.3.4:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com#SS",
		"hysteria2://pass%3Aword@example.com:443?sni=example.com&insecure=1&obfs=salamander&obfs-password=obfspass&upmbps=100&downmbps=500#HY2",
		"tuic://uuid-9900:password@example.com:443?sni=example.com&congestion_control=bbr&udp_relay_mode=quic&allow_insecure=1#TUIC",
		"hysteria://1.2.3.4:36712?auth=secret&peer=example.com&insecure=1&upmbps=50&downmbps=200&obfs=xplus#HY1",
		"anytls://password123@example.com:8443?sni=example.com&insecure=1&fp=chrome#AnyTLS",
		"ssr://MS4yLjMuNDo0NDM6YXV0aF9zaGExX3Y0OmFlcy0yNTYtY2ZiOnRsczEuMl90aWNrZXRfYXV0aDpHdG5CM05uVUEvP29iZnNwYXJhbT0mcHJvdG9wYXJhbT0mcmVtYXJrcz1zc3ItbmlrbmFtZQ==",
		"socks://dXNlcjpwYXNzQDEuMi4zLjQ6MTA4MA==#SOCKS",
		"wireguard://privKEY@1.2.3.4:51820?publickey=peerKEY&address=10.0.0.2%2F32&reserved=1%2C2%2C3&mtu=1420#WG",
	}
	for _, uri := range cases {
		n1, err := ParseURI(uri)
		if err != nil {
			t.Errorf("parse error for %s: %v", uri[:30], err)
			continue
		}
		exported, err := NodeToURI(*n1)
		if err != nil {
			t.Errorf("export error for %s: %v", n1.Protocol, err)
			continue
		}
		n2, err := ParseURI(exported)
		if err != nil {
			t.Errorf("re-parse error for %s (%s...): %v", n1.Protocol, exported[:40], err)
			continue
		}
		if n2.Address != n1.Address || n2.Port != n1.Port || n2.Name != n1.Name {
			t.Errorf("round-trip mismatch %s: %s:%d(%s) → %s:%d(%s)",
				n1.Protocol, n1.Address, n1.Port, n1.Name, n2.Address, n2.Port, n2.Name)
		}
		preview := exported
		if len(preview) > 60 {
			preview = preview[:60]
		}
		t.Logf("OK round-trip %-10s %s", n1.Protocol, preview)
	}
}

func TestExportVMessRoundTrip(t *testing.T) {
	uri := "vmess://eyJ2IjoiMiIsInBzIjoi5rWL6K+VVM0yIiwiYWRkIjoiMS4yLjMuNCIsInBvcnQiOjQ0MywiaWQiOiJhYWFhYWFhYS1iYmJiLWNjY2MtZGRkZC1lZWVlZWVlZWVlZWUiLCJhaWQiOjAsInNjeSI6ImF1dG8iLCJuZXQiOiJ3cyIsInR5cGUiOiIiLCJob3N0IjoiZXhhbXBsZS5jb20iLCJwYXRoIjoiL3BhdGgiLCJ0bHMiOiJ0bHMiLCJzbmkiOiJleGFtcGxlLmNvbSIsImFscG4iOiJodHRwLzEuMSIsImZwIjoiY2hyb21lIn0="
	n1, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("vmess parse error: %v", err)
	}
	exported, err := NodeToURI(*n1)
	if err != nil {
		t.Fatalf("vmess export error: %v", err)
	}
	n2, err := ParseURI(exported)
	if err != nil {
		t.Fatalf("vmess re-parse error: %v (%s)", err, exported)
	}
	if n2.VMess.UUID != n1.VMess.UUID || n2.VMess.Fingerprint != "chrome" || !n2.VMess.TLS {
		t.Errorf("vmess round-trip mismatch: %+v vs %+v", n2.VMess, n1.VMess)
	}
	if n2.VMess.Transport == nil || n2.VMess.Transport.Type != "ws" || n2.VMess.Transport.Host != "example.com" {
		t.Errorf("vmess transport round-trip mismatch: %+v", n2.VMess.Transport)
	}
}

func TestParseSingBoxJSONRawPassthrough(t *testing.T) {
	content := `{
	  "outbounds": [
	    {"type": "direct", "tag": "direct"},
	    {"type": "selector", "tag": "sel", "outbounds": []},
	    {"type": "ssh", "tag": "my-ssh", "server": "1.2.3.4", "server_port": 22, "user": "root", "password": "pw"},
	    {"type": "shadowtls", "tag": "my-stls", "server": "1.2.3.4", "server_port": 443,
	     "password": "pw", "tls": {"enabled": true, "server_name": "example.com",
	     "utls": {"enabled": true, "fingerprint": "chrome"},
	     "ech": {"enabled": true, "config": "ECHCFG"}}},
	    {"type": "vless", "tag": "my-vless", "server": "5.6.7.8", "server_port": 443,
	     "uuid": "uuid-x", "flow": "xtls-rprx-vision",
	     "tls": {"enabled": true, "server_name": "example.com", "insecure": true,
	             "utls": {"enabled": true, "fingerprint": "firefox"},
	             "reality": {"enabled": true, "public_key": "PBK", "short_id": "SID"}},
	     "transport": {"type": "ws", "path": "/wspath", "headers": {"Host": "example.com"}}}
	  ]
	}`
	nodes, err := ParseContent(content)
	if err != nil {
		t.Fatalf("singbox json parse error: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
	var ssh, stls, vless *Node
	for i := range nodes {
		switch nodes[i].Protocol {
		case "ssh":
			ssh = &nodes[i]
		case "shadowtls":
			stls = &nodes[i]
		case "vless":
			vless = &nodes[i]
		}
	}
	if ssh == nil || ssh.RawOutbound == nil {
		t.Errorf("ssh node should keep RawOutbound")
	}
	if stls == nil || stls.RawOutbound == nil {
		t.Errorf("shadowtls node should keep RawOutbound")
	}
	if vless == nil || vless.VLESS == nil {
		t.Fatalf("vless node missing config")
	}
	if vless.VLESS.PublicKey != "PBK" || vless.VLESS.ShortID != "SID" {
		t.Errorf("vless reality fields wrong: %+v", vless.VLESS)
	}
	if vless.VLESS.Insecure != true {
		t.Errorf("vless insecure wrong")
	}
	if vless.VLESS.Transport == nil || vless.VLESS.Transport.Host != "example.com" {
		t.Errorf("vless ws host wrong")
	}
}

func TestParseBase64Subscription(t *testing.T) {
	// base64 of a URI list (trojan + vless)
	list := "trojan://pass@1.2.3.4:443?sni=a.com#T1\nvless://uid@1.2.3.4:443?type=ws&security=tls&sni=a.com&path=%2Fv#V1"
	b64 := base64URLEncode(list)
	nodes, err := ParseContent(b64)
	if err != nil {
		t.Fatalf("base64 sub parse error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(nodes))
	}
}

func TestParseClashYAML(t *testing.T) {
	content := `
proxies:
  - name: "hy2-yaml"
    type: hysteria2
    server: 1.2.3.4
    port: 443
    password: pw
    sni: a.com
    skip-cert-verify: true
    obfs: salamander
    obfs-password: op
    up: 100 Mbps
    down: 500 Mbps
  - name: "ssr-yaml"
    type: ssr
    server: 1.2.3.4
    port: 443
    cipher: aes-256-cfb
    password: "pass"
    protocol: auth_sha1_v4
    obfs: tls1.2_ticket_auth
  - name: "anytls-yaml"
    type: anytls
    server: 1.2.3.4
    port: 8443
    password: pw
    sni: a.com
    skip-cert-verify: true
  - name: "wg-yaml"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    private-key: priv
    public-key: pub
    ip: 10.0.0.2/32
    reserved: [1, 2, 3]
    mtu: 1420
  - name: "socks-yaml"
    type: socks5
    server: 1.2.3.4
    port: 1080
    username: u
    password: p
`
	nodes, err := ParseContent(content)
	if err != nil {
		t.Fatalf("clash yaml parse error: %v", err)
	}
	if len(nodes) != 5 {
		t.Fatalf("want 5 nodes, got %d", len(nodes))
	}
	var hy2, ssr, at, wg, socks *Node
	for i := range nodes {
		switch nodes[i].Protocol {
		case "hysteria2":
			hy2 = &nodes[i]
		case "ssr":
			ssr = &nodes[i]
		case "anytls":
			at = &nodes[i]
		case "wireguard":
			wg = &nodes[i]
		case "socks":
			socks = &nodes[i]
		}
	}
	if hy2 == nil || hy2.Hysteria2.UpMbps != 100 || hy2.Hysteria2.ObfsPassword != "op" {
		t.Errorf("hy2 yaml fields wrong: %+v", hy2)
	}
	if ssr == nil || ssr.SSR.Obfs != "tls1.2_ticket_auth" {
		t.Errorf("ssr yaml fields wrong")
	}
	if at == nil || !at.AnyTLS.Insecure {
		t.Errorf("anytls yaml fields wrong")
	}
	if wg == nil || len(wg.WireGuard.Reserved) != 3 || wg.WireGuard.MTU != 1420 {
		t.Errorf("wg yaml fields wrong: %+v", wg)
	}
	if socks == nil || socks.Socks.Username != "u" {
		t.Errorf("socks yaml fields wrong")
	}
}
