package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	// 记录 JSON 文件中 bool 字段是否真实存在（不序列化），
	// 用于区分"旧文件缺字段"与"用户显式关闭"。
	exitDisableProxySet bool
	tunStrictRouteSet   bool
}

// Defaults 返回一份全新默认设置。
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
	}
}

// Normalize 为零值字段补默认值（供外部包在保存前调用）。
func (s *Settings) Normalize() { s.applyDefaults() }

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
	// 旧版 settings.json 迁移：只有 config_path，按扩展名归入对应内核的记忆路径。
	if m.Settings.ConfigPathSingBox == "" && m.Settings.ConfigPathMihomo == "" && m.Settings.ConfigPath != "" {
		ext := strings.ToLower(filepath.Ext(m.Settings.ConfigPath))
		if ext == ".yaml" || ext == ".yml" {
			m.Settings.ConfigPathMihomo = m.Settings.ConfigPath
		} else {
			m.Settings.ConfigPathSingBox = m.Settings.ConfigPath
		}
	}
	// 路径迁移：配置文件路径只存 configs 目录下的文件名（历史版本存绝对路径，
	// 移动程序目录后会失效），加载时统一归一化为文件名，完整路径使用时再拼接。
	for _, p := range []*string{&m.Settings.ConfigPath, &m.Settings.ConfigPathSingBox, &m.Settings.ConfigPathMihomo} {
		*p = normalizeConfigPath(*p)
	}
	m.Settings.applyDefaults()
	return nil
}

// normalizeConfigPath 把历史存储的配置路径归一化为 configs 目录下的文件名。
// 绝对路径或含分隔符的相对路径只取文件名（configs 目录位置固定，文件名即可定位）。
func normalizeConfigPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) || strings.ContainsAny(p, `/\`) {
		return filepath.Base(p)
	}
	return p
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
