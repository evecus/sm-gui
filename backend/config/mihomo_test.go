package config

import (
	"os"
	"path/filepath"
	"testing"

	"sm-gui/backend/node"
)

const mihomoTestCfg = `mixed-port: 7890
proxies:
  - name: proxy
    type: ss
    server: old.example.com
    port: 8388
    cipher: aes-128-gcm
    password: oldpass
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - proxy
      - DIRECT
rules:
  - MATCH,PROXY
`

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyNodeToMihomoConfig(t *testing.T) {
	p := writeTempYAML(t, mihomoTestCfg)
	n := node.Node{
		ID: "n1", Name: "test-hy2", Address: "1.2.3.4", Port: 443, Protocol: "hysteria2",
		Hysteria2: &node.Hysteria2Config{
			Password: "pw", SNI: "sni.example.com", UpMbps: 30, DownMbps: 200, Insecure: true,
		},
	}
	if err := ApplyNodeToMihomoConfig(p, n); err != nil {
		t.Fatalf("apply node: %v", err)
	}

	cfg, err := loadYAML(p)
	if err != nil {
		t.Fatal(err)
	}
	proxies := getProxies(cfg)
	if len(proxies) != 1 {
		t.Fatalf("proxies 数量 = %d, 期望 1", len(proxies))
	}
	pr := proxies[0].(map[string]interface{})
	if pr["name"] != mihomoProxyName || pr["type"] != "hysteria2" || pr["server"] != "1.2.3.4" {
		t.Fatalf("proxies 条目不符: %v", pr)
	}
	if pr["password"] != "pw" || pr["up"] != 30 || pr["down"] != 200 {
		t.Fatalf("hysteria2 字段不符: %v", pr)
	}
	if pr["skip-cert-verify"] != true {
		t.Fatalf("skip-cert-verify 应为 true: %v", pr)
	}

	// PROXY 组仍引用 proxy
	groups, _ := cfg["proxy-groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("proxy-groups 数量 = %d, 期望 1", len(groups))
	}
	g := groups[0].(map[string]interface{})
	members, _ := g["proxies"].([]interface{})
	found := false
	for _, m := range members {
		if s, ok := m.(string); ok && s == mihomoProxyName {
			found = true
		}
	}
	if !found {
		t.Fatalf("PROXY 组未引用 proxy: %v", members)
	}

	// 已应用节点可被识别
	if got := FindAppliedNodeID(CoreMihomo, p, []node.Node{n}); got != "n1" {
		t.Fatalf("FindAppliedNodeID = %q, 期望 n1", got)
	}
	// 其他节点不应误判
	other := n
	other.ID = "n2"
	other.Hysteria2.Password = "different"
	if got := FindAppliedNodeID(CoreMihomo, p, []node.Node{other}); got != "" {
		t.Fatalf("不同节点被误判为已应用: %q", got)
	}
}

// TestMihomoRawClashProxyRoundTrip: Clash YAML 导入的节点无损回写。
func TestMihomoRawClashProxyRoundTrip(t *testing.T) {
	p := writeTempYAML(t, mihomoTestCfg)
	clashYAML := `proxies:
  - name: raw-node
    type: vless
    server: 5.6.7.8
    port: 443
    uuid: uuid-123
    tls: true
    servername: example.com
    network: ws
    client-fingerprint: chrome
    ws-opts:
      path: /ws
      headers:
        Host: example.com
`
	nodes, err := node.ParseContent(clashYAML)
	if err != nil {
		t.Fatalf("parse clash yaml: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("解析节点数 = %d", len(nodes))
	}
	n := nodes[0]
	if n.RawClashProxy == nil {
		t.Fatal("Clash YAML 导入的节点应保留原始条目")
	}
	if err := ApplyNodeToMihomoConfig(p, n); err != nil {
		t.Fatalf("apply node: %v", err)
	}
	cfg, _ := loadYAML(p)
	proxies := getProxies(cfg)
	if len(proxies) != 1 {
		t.Fatalf("proxies 数量 = %d, 期望 1", len(proxies))
	}
	pr := proxies[0].(map[string]interface{})
	if pr["name"] != mihomoProxyName || pr["uuid"] != "uuid-123" || pr["servername"] != "example.com" {
		t.Fatalf("原始字段丢失: %v", pr)
	}
	ws, ok := pr["ws-opts"].(map[string]interface{})
	if !ok || ws["path"] != "/ws" {
		t.Fatalf("ws-opts 丢失: %v", pr["ws-opts"])
	}
	if got := FindAppliedNodeID(CoreMihomo, p, []node.Node{n}); got != n.ID {
		t.Fatalf("FindAppliedNodeID = %q, 期望 %q", got, n.ID)
	}
}

func TestMihomoTunAndMixed(t *testing.T) {
	p := writeTempYAML(t, mihomoTestCfg)

	// TUN 开启
	if err := SetTun(CoreMihomo, p, true, "system", 8500, true); err != nil {
		t.Fatalf("set tun: %v", err)
	}
	if !HasTunInbound(CoreMihomo, p) {
		t.Fatal("TUN 应为开启状态")
	}
	cfg, _ := loadYAML(p)
	tun := cfg["tun"].(map[string]interface{})
	if tun["stack"] != "system" || tun["mtu"] != 8500 || tun["enable"] != true {
		t.Fatalf("tun 字段不符: %v", tun)
	}
	if _, ok := tun["strict_route"]; ok {
		t.Fatal("mihomo 不应有 strict_route 字段")
	}
	// TUN 关闭
	if err := SetTun(CoreMihomo, p, false, "", 0, false); err != nil {
		t.Fatalf("unset tun: %v", err)
	}
	if HasTunInbound(CoreMihomo, p) {
		t.Fatal("TUN 应为关闭状态")
	}

	// mixed-port + allow-lan
	if err := SetMixedInbound(CoreMihomo, p, true, "0.0.0.0", 2081); err != nil {
		t.Fatalf("set mixed: %v", err)
	}
	cfg, _ = loadYAML(p)
	if cfg["mixed-port"] != 2081 {
		t.Fatalf("mixed-port = %v, 期望 2081", cfg["mixed-port"])
	}
	if cfg["allow-lan"] != true {
		t.Fatalf("allow-lan = %v, 期望 true", cfg["allow-lan"])
	}
	if cfg["bind-address"] != "*" {
		t.Fatalf("bind-address = %v, 期望 *", cfg["bind-address"])
	}
	// 关闭
	if err := SetMixedInbound(CoreMihomo, p, false, "127.0.0.1", 0); err != nil {
		t.Fatalf("unset mixed: %v", err)
	}
	cfg, _ = loadYAML(p)
	if _, ok := cfg["mixed-port"]; ok {
		t.Fatal("mixed-port 应被删除")
	}
}

func TestMihomoProxyGroupAutoCreate(t *testing.T) {
	// 无 proxy-groups 的配置：应用节点后应自动创建引用 proxy 的 PROXY 组
	p := writeTempYAML(t, "port: 7890\n")
	n := node.Node{
		ID: "n1", Name: "ss-node", Address: "1.1.1.1", Port: 8388, Protocol: "ss",
		SS: &node.SSConfig{Method: "aes-128-gcm", Password: "pw"},
	}
	if err := ApplyNodeToMihomoConfig(p, n); err != nil {
		t.Fatalf("apply node: %v", err)
	}
	cfg, _ := loadYAML(p)
	groups, _ := cfg["proxy-groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("proxy-groups 应自动创建, 实际 %d 个", len(groups))
	}
	g := groups[0].(map[string]interface{})
	if g["name"] != mihomoGroupName || g["type"] != "select" {
		t.Fatalf("自动创建的组不符: %v", g)
	}
	if got := FindAppliedNodeID(CoreMihomo, p, []node.Node{n}); got != "n1" {
		t.Fatalf("FindAppliedNodeID = %q, 期望 n1", got)
	}
}
