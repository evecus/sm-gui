package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

// 支持的内核。
const (
	CoreSingBox = "sing-box"
	CoreMihomo  = "mihomo"
)

// Settings 持久化设置。新增字段必须同时在 applyDefaults 里给默认值，
// 否则旧 settings.json 升级后会出现零值。
type Settings struct {
	// 内核：sing-box | mihomo
	Core string `json:"core"`

	// 旧版字段：保留并同步为“当前内核选中的配置文件”，前端直接读取它回显。
	ConfigPath string `json:"config_path"`

	// 两个内核各自记忆的配置文件路径（切换内核时互不丢失）。
	ConfigPathSingBox string `json:"config_path_singbox,omitempty"`
	ConfigPathMihomo  string `json:"config_path_mihomo,omitempty"`

	Subscriptions []string `json:"subscriptions"`

	// 当前应用的节点 ID（切配置文件后据此重新应用）
	AppliedNodeID string `json:"applied_node_id,omitempty"`

	// 路由模式：custom（跟随用户配置文件）| bypass（内置:绕过大陆）|
	// blacklist（内置:GFW列表）| global（内置:全局代理）。
	// 内置模式下配置由程序模板合成，直接写入 run 目录，不碰用户 configs 文件。
	RoutingMode string `json:"routing_mode"`

	// TUN 开关持久化状态（内置模式据此生成 tun inbound；custom 模式与配置文件保持同步）。
	TunEnabled bool `json:"tun_enabled"`

	// 系统代理
	ProxyListen      string `json:"proxy_listen"`       // mixed inbound 监听地址
	ProxyPort        int    `json:"proxy_port"`         // mixed inbound 监听端口
	ExitDisableProxy bool   `json:"exit_disable_proxy"` // 退出程序时自动关闭系统代理

	// TUN 模式
	TunStack       string `json:"tun_stack"`        // gvisor | system | mixed
	TunMTU         int    `json:"tun_mtu"`          // TUN 网卡 MTU
	TunStrictRoute bool   `json:"tun_strict_route"` // sing-box strict_route

	// 订阅
	SubUserAgent  string `json:"sub_user_agent"`  // 拉取订阅时的 User-Agent
	SubTimeoutSec int    `json:"sub_timeout_sec"` // 拉取订阅超时秒数

	// 日志与界面
	LogMaxLines    int `json:"log_max_lines"`    // 运行日志最大保留行数
	PollIntervalMs int `json:"poll_interval_ms"` // 前端状态轮询间隔(毫秒)

	// 启动
	AutoStart   bool `json:"auto_start"`   // 开机自启动（管理员运行时注册计划任务以最高权限自启，否则写注册表 Run 键）
	SilentStart bool `json:"silent_start"` // 自启动时静默：不显示主窗口，仅托盘图标（手动启动不受影响）

	// 内置路由模式的可配置参数（合成 run 配置时使用，custom 模式不涉及）
	Builtin BuiltinSettings `json:"builtin"`

	// 记录 JSON 文件中 bool 字段是否真实存在（不序列化），
	// 用于区分"旧文件缺字段"与"用户显式关闭"。
	exitDisableProxySet bool
	tunStrictRouteSet   bool
}

// DNSServer sing-box DNS 服务器（类型/地址/端口/路径）。
// 地址必填；端口、路径选填，零值时按类型补默认端口 / 不写入。
type DNSServer struct {
	Type    string `json:"type"`    // tcp | udp | tls | https | quic
	Address string `json:"address"` // IP 或域名（tls/https 建议域名）
	Port    int    `json:"port"`    // 0 = 按类型默认端口
	Path    string `json:"path"`    // https 类型的 URL 路径
}

// ClashAPIConfig clash-api 监听配置（双内核共用）。
type ClashAPIConfig struct {
	Listen string `json:"listen"` // 127.0.0.1 | 0.0.0.0
	Port   int    `json:"port"`
	Secret string `json:"secret"`
}

// BuiltinSettings 内置路由模式可配置参数（settings.json 的 builtin 段）。
// 默认值即此前写死的值，旧 settings.json 缺段时自动补默认。
// 注意：ClashAPIDisabled 用反转语义——零值 false 即默认启用，无需缺字段探测。
type BuiltinSettings struct {
	LogLevel        string         `json:"log_level"` // debug | info | warning | error
	DNSMode         string         `json:"dns_mode"`  // redir-host | fake-ip
	IPv6            bool           `json:"ipv6"`
	ClashAPIDisabled bool          `json:"clash_api_disabled"` // true 时完全不生成 clash-api 配置
	ResolverDNS       string         `json:"resolver_dns"`        // 解析 DNS 主服务器（必须是 IP）；sing-box 取此条，mihomo 对应 default_nameserver
	ResolverDNSBackup string         `json:"resolver_dns_backup"` // 解析 DNS 备用服务器（必须是 IP，默认 119.29.29.29）；仅 mihomo 填两个
	ClashAPI        ClashAPIConfig `json:"clash_api"`
	SingBoxDirect   DNSServer      `json:"singbox_direct"` // sing-box 直连 DNS
	SingBoxProxy    DNSServer      `json:"singbox_proxy"`  // sing-box 代理 DNS
	MihomoDirect    []string       `json:"mihomo_direct"`  // mihomo 直连 DNS（两个）
	MihomoProxy     []string       `json:"mihomo_proxy"`   // mihomo 代理 DNS（两个，生成时自动加 #PROXY）
}

// DefaultBuiltin 返回内置路由参数的默认设置（与写死版本行为一致）。
func DefaultBuiltin() BuiltinSettings {
	return BuiltinSettings{
		LogLevel:          "warning",
		DNSMode:           "redir-host",
		ResolverDNS:       "223.5.5.5",
		ResolverDNSBackup: "119.29.29.29",
		ClashAPI:          ClashAPIConfig{Listen: "127.0.0.1", Port: 9090},
		SingBoxDirect: DNSServer{Type: "udp", Address: "223.5.5.5", Port: 53},
		SingBoxProxy:  DNSServer{Type: "udp", Address: "8.8.8.8", Port: 53},
		MihomoDirect:  []string{"223.5.5.5", "119.29.29.29"},
		MihomoProxy:   []string{"1.1.1.1", "8.8.8.8"},
	}
}

// Defaults 返回一份全新默认设置（含内置路由参数——
// 此前全新安装漏填 Builtin 段，导致设置界面显示全 0）。
func Defaults() Settings {
	return Settings{
		Core:             CoreSingBox,
		RoutingMode:      ModeCustom,
		Subscriptions:    []string{},
		ProxyListen:      "127.0.0.1",
		ProxyPort:        2080,
		ExitDisableProxy: true,
		TunStack:         "gvisor",
		TunMTU:           9000,
		TunStrictRoute:   true,
		SubUserAgent:     "clash.meta",
		SubTimeoutSec:    30,
		LogMaxLines:      500,
		PollIntervalMs:   2000,
		Builtin:          DefaultBuiltin(),
	}
}

// Normalize 为零值字段补默认值（供外部包在保存前调用）。
func (s *Settings) Normalize() {
	s.applyDefaults()
	// mihomo DNS 列表去掉空白项
	s.Builtin.MihomoDirect = trimNonEmpty(s.Builtin.MihomoDirect)
	s.Builtin.MihomoProxy = trimNonEmpty(s.Builtin.MihomoProxy)
}

func trimNonEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, v := range ss {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// applyDefaults 为零值字段补默认值（兼容旧版 settings.json）。
func (s *Settings) applyDefaults() {
	def := Defaults()
	if s.Core == "" {
		s.Core = def.Core
	}
	if s.RoutingMode == "" {
		s.RoutingMode = ModeCustom
	}
	if s.Subscriptions == nil {
		s.Subscriptions = []string{}
	}
	if strings.TrimSpace(s.ProxyListen) == "" {
		s.ProxyListen = def.ProxyListen
	}
	if s.ProxyPort <= 0 {
		s.ProxyPort = def.ProxyPort
	}
	if s.TunStack == "" {
		s.TunStack = def.TunStack
	}
	if s.TunMTU <= 0 {
		s.TunMTU = def.TunMTU
	}
	if strings.TrimSpace(s.SubUserAgent) == "" {
		s.SubUserAgent = def.SubUserAgent
	}
	if s.SubTimeoutSec <= 0 {
		s.SubTimeoutSec = def.SubTimeoutSec
	}
	if s.LogMaxLines <= 0 {
		s.LogMaxLines = def.LogMaxLines
	}
	if s.PollIntervalMs <= 0 {
		s.PollIntervalMs = def.PollIntervalMs
	}
	// 内置路由参数逐字段补默认
	b := &s.Builtin
	d := DefaultBuiltin()
	if b.LogLevel == "" {
		b.LogLevel = d.LogLevel
	}
	if b.DNSMode == "" {
		b.DNSMode = d.DNSMode
	}
	if b.ResolverDNS == "" {
		b.ResolverDNS = d.ResolverDNS
	}
	if b.ResolverDNSBackup == "" {
		b.ResolverDNSBackup = d.ResolverDNSBackup
	}
	if b.ClashAPI.Listen == "" {
		b.ClashAPI.Listen = d.ClashAPI.Listen
	}
	if b.ClashAPI.Port == 0 {
		b.ClashAPI.Port = d.ClashAPI.Port
	}
	if b.SingBoxDirect.Type == "" && b.SingBoxDirect.Address == "" {
		b.SingBoxDirect = d.SingBoxDirect
	}
	if b.SingBoxProxy.Type == "" && b.SingBoxProxy.Address == "" {
		b.SingBoxProxy = d.SingBoxProxy
	}
	if len(b.MihomoDirect) == 0 {
		b.MihomoDirect = d.MihomoDirect
	}
	if len(b.MihomoProxy) == 0 {
		b.MihomoProxy = d.MihomoProxy
	}
	// bool 零值为 false，但 ExitDisableProxy / TunStrictRoute 的默认值是 true。
	// 由于旧文件中不存在这两个字段，无法区分"显式关闭"与"未设置"，
	// 用指针在 unmarshal 前标记是否存在。
	if !s.exitDisableProxySet {
		s.ExitDisableProxy = def.ExitDisableProxy
	}
	if !s.tunStrictRouteSet {
		s.TunStrictRoute = def.TunStrictRoute
	}
}

// ActiveConfigPath 返回当前内核记忆的配置文件名（configs 目录下，非完整路径）。
func (s *Settings) ActiveConfigPath() string {
	if s.Core == CoreMihomo {
		return s.ConfigPathMihomo
	}
	return s.ConfigPathSingBox
}

// SetCoreConfigPath 记录指定内核的配置文件名（同时同步旧字段 ConfigPath）。
func (s *Settings) SetCoreConfigPath(core, name string) {
	if core == CoreMihomo {
		s.ConfigPathMihomo = name
	} else {
		s.ConfigPathSingBox = name
	}
	s.ConfigPath = name
}

// Validate 校验设置合法性（保存前调用）。
func (s *Settings) Validate() error {
	switch s.Core {
	case CoreSingBox, CoreMihomo:
	default:
		return fmt.Errorf("内核必须是 sing-box / mihomo")
	}
	switch s.RoutingMode {
	case ModeCustom, ModeBypass, ModeBlacklist, ModeGlobal:
	default:
		return fmt.Errorf("路由模式必须是 custom / bypass / blacklist / global")
	}
	if strings.TrimSpace(s.ProxyListen) == "" {
		return fmt.Errorf("监听地址不能为空")
	}
	if s.ProxyPort < 1 || s.ProxyPort > 65535 {
		return fmt.Errorf("代理端口必须在 1-65535 之间")
	}
	switch s.TunStack {
	case "gvisor", "system", "mixed":
	default:
		return fmt.Errorf("TUN 协议栈必须是 gvisor / system / mixed")
	}
	if s.TunMTU < 576 || s.TunMTU > 65535 {
		return fmt.Errorf("TUN MTU 必须在 576-65535 之间")
	}
	if strings.TrimSpace(s.SubUserAgent) == "" {
		return fmt.Errorf("订阅 User-Agent 不能为空")
	}
	if s.SubTimeoutSec < 1 || s.SubTimeoutSec > 600 {
		return fmt.Errorf("订阅超时必须在 1-600 秒之间")
	}
	if s.LogMaxLines < 50 || s.LogMaxLines > 100000 {
		return fmt.Errorf("日志行数必须在 50-100000 之间")
	}
	if s.PollIntervalMs < 500 || s.PollIntervalMs > 60000 {
		return fmt.Errorf("轮询间隔必须在 500-60000 毫秒之间")
	}
	// 内置路由参数
	switch s.Builtin.LogLevel {
	case "debug", "info", "warning", "error":
	default:
		return fmt.Errorf("日志等级必须是 debug / info / warning / error")
	}
	switch s.Builtin.DNSMode {
	case "redir-host", "fake-ip":
	default:
		return fmt.Errorf("DNS 模式必须是 redir-host / fake-ip")
	}
	if net.ParseIP(strings.TrimSpace(s.Builtin.ResolverDNS)) == nil {
		return fmt.Errorf("解析 DNS 服务器必须是 IP 地址")
	}
	if net.ParseIP(strings.TrimSpace(s.Builtin.ResolverDNSBackup)) == nil {
		return fmt.Errorf("解析 DNS 备用服务器必须是 IP 地址")
	}
	if s.Builtin.ClashAPI.Listen != "127.0.0.1" && s.Builtin.ClashAPI.Listen != "0.0.0.0" {
		return fmt.Errorf("clash-api 监听地址必须是 127.0.0.1 / 0.0.0.0")
	}
	if s.Builtin.ClashAPI.Port < 1 || s.Builtin.ClashAPI.Port > 65535 {
		return fmt.Errorf("clash-api 端口必须在 1-65535 之间")
	}
	for _, ds := range []struct {
		name string
		svr  DNSServer
	}{{"直连 DNS", s.Builtin.SingBoxDirect}, {"代理 DNS", s.Builtin.SingBoxProxy}} {
		switch ds.svr.Type {
		case "tcp", "udp", "tls", "https", "quic":
		default:
			return fmt.Errorf("sing-box %s 类型必须是 tcp / udp / tls / https / quic", ds.name)
		}
		if strings.TrimSpace(ds.svr.Address) == "" {
			return fmt.Errorf("sing-box %s 地址不能为空", ds.name)
		}
	}
	for _, l := range []struct {
		name string
		list []string
	}{{"直连 DNS", s.Builtin.MihomoDirect}, {"代理 DNS", s.Builtin.MihomoProxy}} {
		if len(l.list) == 0 {
			return fmt.Errorf("mihomo %s 至少填写一个", l.name)
		}
		for _, v := range l.list {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("mihomo %s 不能为空", l.name)
			}
		}
	}
	return nil
}

type Manager struct {
	mu       sync.RWMutex
	Settings Settings
	path     string
}

// settingsAlias 借助指针字段探测 JSON 中 bool 字段是否真实存在。
type settingsAlias struct {
	Settings
	ExitDisableProxy *bool `json:"exit_disable_proxy"`
	TunStrictRoute   *bool `json:"tun_strict_route"`
}

func NewManager(path string) *Manager {
	return &Manager{path: path}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.Settings = Defaults()
			m.Settings.applyDefaults() // 双保险：任何缺省段都补齐
			return nil
		}
		return err
	}
	var alias settingsAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	m.Settings = alias.Settings
	m.Settings.exitDisableProxySet = alias.ExitDisableProxy != nil
	m.Settings.tunStrictRouteSet = alias.TunStrictRoute != nil
	m.Settings.applyDefaults()
	return nil
}

func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.Settings.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.Settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0644)
}
