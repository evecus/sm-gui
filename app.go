package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"sm-gui/backend/config"
	"sm-gui/backend/node"
	"sm-gui/backend/probe"
	"sm-gui/backend/singbox"
	"sm-gui/backend/sysproxy"
	"sm-gui/backend/winutil"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx        context.Context
	nodeStore  *node.Store
	cfgManager *config.Manager
	sbProcess  *singbox.Process
	proxy      *sysproxy.Manager
	autoStop   chan struct{} // 分组订阅自动更新循环的停止信号
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 启动系统托盘（左键显示主窗口，右键菜单可退出）
	setupTray(ctx)

	// if startup itself panics, record it instead of dying silently
	defer func() {
		if r := recover(); r != nil {
			appendCrashLog(fmt.Sprintf("===== %s | startup panic: %v =====\n%s\n",
				time.Now().Format("2006-01-02 15:04:05"), r, debug.Stack()))
		}
	}()

	// init data dir
	dataDir := getDataDir()
	os.MkdirAll(dataDir, 0755)
	// configs 目录(与 data 同级, 存放内核配置文件: sing-box json / mihomo yaml)
	os.MkdirAll(getConfigsDir(), 0755)
	// run 目录(核心运行目录, 存放复制的配置文件与 mihomo geodata)
	ensureRunDir()

	// SQLite 节点存储
	a.nodeStore = node.NewStore(filepath.Join(dataDir, "nodes.db"))
	a.cfgManager = config.NewManager(filepath.Join(dataDir, "settings.json"))
	a.proxy = sysproxy.NewManager()

	a.nodeStore.Load()
	a.cfgManager.Load()

	// sing-box 进程（日志上限取自设置）
	a.sbProcess = singbox.NewProcess(a.cfgManager.Settings.LogMaxLines)

	// 分组订阅自动更新循环（每 10 分钟检查一次各分组是否到期）
	a.startAutoUpdateLoop()

	// 提权重启后的状态恢复（TUN 模式需要管理员权限时曾申请提权重启）
	go a.restoreAfterElevation()
}

func (a *App) shutdown(ctx context.Context) {
	// 移除托盘图标
	stopTray()

	// 退出时按设置还原系统代理（需在杀掉 sing-box 前执行，避免残留）
	if a.cfgManager != nil && a.cfgManager.Settings.ExitDisableProxy && a.proxy != nil {
		a.proxy.Disable()
	}
	// cleanup on exit: kill singbox if running
	a.sbProcess.Stop()
	// stop the group auto-update loop
	if a.autoStop != nil {
		close(a.autoStop)
		a.autoStop = nil
	}
	// close node database
	if a.nodeStore != nil {
		a.nodeStore.Close()
	}
}

func getDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

// getConfigsDir returns the configs directory next to the executable
// (same level as the data dir). It holds sing-box config json files.
func getConfigsDir() string {
	return filepath.Join(filepath.Dir(getDataDir()), "configs")
}

// ensureConfigsDir creates the configs dir if missing.
func ensureConfigsDir() {
	os.MkdirAll(getConfigsDir(), 0755)
}

// ─── run 目录 ─────────────────────────────────────────────────────────────────
// run 目录是核心进程的运行目录（位于程序根目录）：
//   - 选择配置文件时，把 configs/ 中的源文件复制进来并改名为
//     config.json（sing-box）或 config.yaml（mihomo）；
//   - 启动内核时 sing-box 用 `run -D run`，mihomo 用 `-d run`
//     （geodata 等数据文件也会落在 run 目录，切换配置时不会被清除）；
//   - 每次切换配置文件都会清掉旧配置，保证 run 目录中只有当前内核的一个配置文件。

func getRunDir() string {
	return filepath.Join(filepath.Dir(getDataDir()), "run")
}

func ensureRunDir() {
	os.MkdirAll(getRunDir(), 0755)
}

// runConfigName 返回 run 目录中当前内核配置文件的固定名。
func runConfigName(core string) string {
	if core == config.CoreMihomo {
		return "config.yaml"
	}
	return "config.json"
}

func runConfigPath(core string) string {
	return filepath.Join(getRunDir(), runConfigName(core))
}

// clearRunConfig 清除 run 目录中的旧配置文件（config.json/yaml/yml 及临时文件），
// 保证 run 目录中只保留当前内核的一个配置文件；geodata 等数据文件不受影响。
func clearRunConfig() {
	ensureRunDir()
	for _, name := range []string{
		"config.json", "config.yaml", "config.yml",
		"config.json.tmp", "config.yaml.tmp", "config.yml.tmp",
	} {
		os.Remove(filepath.Join(getRunDir(), name))
	}
}

// syncRunConfig 把当前配置同步到 run 目录（先清除旧配置）：
//   - custom 模式：把 configs 目录中选中的配置文件原样复制进来；
//   - 内置模式（bypass/blacklist/global）：由模板 + 已应用节点合成，不读用户配置文件。
func (a *App) syncRunConfig(core, srcPath string) error {
	if config.IsBuiltinMode(a.cfgManager.Settings.RoutingMode) {
		return a.generateBuiltinRunConfig(core)
	}
	if srcPath == "" {
		return fmt.Errorf("未选择配置文件")
	}
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}
	clearRunConfig()
	dst := runConfigPath(core)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("写入 run 配置失败: %v", err)
	}
	return nil
}

// getBuiltinRulesDir 内置路由规则文件目录（run/rules，含 srs/ 与 mrs/ 子目录）。
func getBuiltinRulesDir() string {
	return filepath.Join(getRunDir(), "rules")
}

// currentConfigPath 返回当前内核选中配置文件的完整路径。
// Settings 里只存 configs 目录下的文件名（保证移动程序目录后设置仍有效），
// 完整路径在需要读文件时再拼接。
func (a *App) currentConfigPath() string {
	name := a.cfgManager.Settings.ActiveConfigPath()
	if name == "" {
		return ""
	}
	return filepath.Join(getConfigsDir(), name)
}

// generateBuiltinRunConfig 合成内置路由模式的配置并写入 run 目录。
func (a *App) generateBuiltinRunConfig(core string) error {
	s := a.cfgManager.Settings
	// 规则文件校验前置：缺文件时给出明确提示，而不是让内核启动报错
	if err := config.CheckRuleFiles(s.RoutingMode, s.Builtin.DNSMode, getBuiltinRulesDir()); err != nil {
		return err
	}
	var n *node.Node
	if s.AppliedNodeID != "" {
		n = a.nodeStore.Get(s.AppliedNodeID)
	}
	if n == nil {
		return fmt.Errorf("内置路由模式需要先应用一个节点（在节点列表右键 → 应用此节点）")
	}
	opts := config.BuiltinOptions{
		Mode:           s.RoutingMode,
		TunEnabled:     s.TunEnabled,
		TunStack:       s.TunStack,
		TunMTU:         s.TunMTU,
		TunStrictRoute: s.TunStrictRoute,
		ProxyEnabled:   a.proxy.IsEnabled(),
		ProxyListen:    s.ProxyListen,
		ProxyPort:      s.ProxyPort,
		RulesDir:       getBuiltinRulesDir(),
		UIDir:          filepath.Join(getRunDir(), "ui"),
		CachePath:      filepath.Join(getRunDir(), "cache.db"),
		Cfg:            &s.Builtin,
	}
	// clash-api external-ui 目录（sing-box 目录为空时会自动下载默认面板）；关闭时不建
	if !s.Builtin.ClashAPIDisabled {
		os.MkdirAll(filepath.Join(getRunDir(), "ui"), 0755)
	}
	data, err := config.BuildBuiltinConfig(core, opts, n)
	if err != nil {
		return err
	}
	clearRunConfig()
	dst := runConfigPath(core)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("写入 run 配置失败: %v", err)
	}
	return nil
}

// GetConfigFiles lists config files in the configs directory (sorted by name).
// 按当前内核过滤扩展名：sing-box → .json；mihomo → .yaml/.yml。
func (a *App) GetConfigFiles() []string {
	return guardP("GetConfigFiles", func() []string {
		ensureConfigsDir()
		core := config.CoreSingBox
		if a.cfgManager != nil {
			core = a.cfgManager.Settings.Core
		}
		match := map[string]bool{".json": true}
		if core == config.CoreMihomo {
			match = map[string]bool{".yaml": true, ".yml": true}
		}
		entries, err := os.ReadDir(getConfigsDir())
		if err != nil {
			return []string{}
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if match[strings.ToLower(filepath.Ext(e.Name()))] {
				files = append(files, e.Name())
			}
		}
		sort.Strings(files)
		if files == nil {
			files = []string{}
		}
		// 末尾追加内置路由配置项（内置配置：绕过大陆 / GFW列表 / 全局代理）
		files = append(files, config.BuiltinDisplayNames()...)
		return files
	})
}

// SelectConfigFile selects a config from the configs directory by filename.
// 选择后把该文件复制到 run 目录（config.json / config.yaml），供内核启动使用。
func (a *App) SelectConfigFile(name string) (string, error) {
	return guardR("SelectConfigFile", func() (string, error) {
		if a.cfgManager == nil {
			return "", fmt.Errorf("设置尚未就绪，请重启应用")
		}
		// 内置路由配置项：切换 RoutingMode，不涉及用户配置文件
		if mode, ok := config.ParseBuiltinName(name); ok {
			return a.selectBuiltinRouting(mode)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("未指定配置文件")
		}
		// 安全检查: 只允许纯文件名, 禁止路径穿越
		if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || filepath.Base(name) != name {
			return "", fmt.Errorf("非法文件名: %s", name)
		}
		full := filepath.Join(getConfigsDir(), name)
		if st, err := os.Stat(full); err != nil || st.IsDir() {
			return "", fmt.Errorf("配置文件不存在: %s", name)
		}

		s := a.cfgManager.Settings
		core := s.Core
		// 扩展名必须与当前内核匹配
		ext := strings.ToLower(filepath.Ext(name))
		if core == config.CoreMihomo {
			if ext != ".yaml" && ext != ".yml" {
				return "", fmt.Errorf("mihomo 内核需要 yaml 配置文件（.yaml/.yml），不支持: %s", name)
			}
		} else if ext != ".json" {
			return "", fmt.Errorf("sing-box 内核需要 json 配置文件（.json），不支持: %s", name)
		}

		// ── 切换编排：停核心 → 换配置 → 重建节点/inbound/tun → 复制到 run → 拉起核心 ──
		wasRunning := a.sbProcess.GetStatus().Running

		// 切换前探测旧状态（TUN 看持久化开关，custom 模式下它与配置文件保持同步；
		// 系统代理看注册表）
		tunWasOn := s.TunEnabled
		proxyWasOn := a.proxy.IsEnabled()

		if wasRunning {
			if err := a.sbProcess.Stop(); err != nil {
				return "", fmt.Errorf("停止核心失败: %v", err)
			}
		}

		// 选真实配置文件 = 路由回到 custom 模式
		s.RoutingMode = config.ModeCustom
		s.SetCoreConfigPath(core, name)
		a.cfgManager.Settings = s
		if err := a.cfgManager.Save(); err != nil {
			return "", fmt.Errorf("保存设置失败: %v", err)
		}

		// 重新应用节点（如果之前有应用过的节点）
		if s.AppliedNodeID != "" {
			if n := a.nodeStore.Get(s.AppliedNodeID); n != nil {
				if err := config.ApplyNodeToConfig(core, full, *n); err != nil {
					return "", fmt.Errorf("重新应用节点失败: %v", err)
				}
			}
		}

		// 系统代理开着则重建 mixed inbound / mixed-port 并重设注册表（端口可能已变）
		if proxyWasOn {
			if err := config.SetMixedInbound(core, full, true, s.ProxyListen, s.ProxyPort); err != nil {
				return "", fmt.Errorf("重建系统代理配置失败: %v", err)
			}
			if err := a.proxy.Enable("127.0.0.1", s.ProxyPort); err != nil {
				return "", fmt.Errorf("重设系统代理失败: %v", err)
			}
		}

		// TUN 开着则重建 TUN 配置
		if tunWasOn {
			if err := config.SetTun(core, full, true, s.TunStack, s.TunMTU, s.TunStrictRoute); err != nil {
				return "", fmt.Errorf("重建 TUN 配置失败: %v", err)
			}
		}

		// 复制到 run 目录（先清除旧配置，保证只有一个当前内核的配置文件）
		if err := a.syncRunConfig(core, full); err != nil {
			return "", fmt.Errorf("同步 run 配置失败: %v", err)
		}

		// 切换前核心在跑则重新拉起
		if wasRunning {
			if err := a.startCore(); err != nil {
				return "", fmt.Errorf("配置已切换，但核心启动失败: %v（请手动启动核心）", err)
			}
		}
		return full, nil
	})
}

// selectBuiltinRouting 切换到内置路由模式（bypass / blacklist / global）。
// 用户 configs/ 配置文件与其各内核记忆路径不动（切回真实文件时原样恢复）；
// ConfigPath 置空以便前端下拉正确回显内置项。配置由模板合成写入 run 目录。
// 节点不是前置条件：未应用节点时只记录模式，等应用节点 / 启动核心时再合成，
// 保证「先选内置配置、再应用节点」的顺序同样可用。
func (a *App) selectBuiltinRouting(mode string) (string, error) {
	s := a.cfgManager.Settings
	core := s.Core
	// 前置校验：规则文件（与节点无关，缺失早提示）
	if err := config.CheckRuleFiles(mode, s.Builtin.DNSMode, getBuiltinRulesDir()); err != nil {
		return "", err
	}
	wasRunning := a.sbProcess.GetStatus().Running
	if wasRunning {
		if err := a.sbProcess.Stop(); err != nil {
			return "", fmt.Errorf("停止核心失败: %v", err)
		}
	}
	s.RoutingMode = mode
	s.ConfigPath = "" // 清空当前路径回显，各内核记忆路径保留
	a.cfgManager.Settings = s
	if err := a.cfgManager.Save(); err != nil {
		return "", fmt.Errorf("保存设置失败: %v", err)
	}
	// 系统代理开着：先设注册表再合成（合成时按注册表状态写入 mixed inbound）
	if a.proxy.IsEnabled() {
		if err := a.proxy.Enable("127.0.0.1", s.ProxyPort); err != nil {
			return "", fmt.Errorf("重设系统代理失败: %v", err)
		}
	}
	// 已应用节点才立即合成；未应用则等 ApplyNode / 启动核心时合成
	if s.AppliedNodeID != "" && a.nodeStore.Get(s.AppliedNodeID) != nil {
		if err := a.generateBuiltinRunConfig(core); err != nil {
			return "", err
		}
	}
	if wasRunning {
		if err := a.startCore(); err != nil {
			return "", fmt.Errorf("路由模式已切换，但核心启动失败: %v（请手动启动核心）", err)
		}
	}
	display, _ := config.BuiltinDisplayName(mode)
	return display, nil
}

// OpenConfigsDir opens the configs directory in Windows Explorer.
func (a *App) OpenConfigsDir() error {
	return guardE("OpenConfigsDir", func() error {
		ensureConfigsDir()
		return exec.Command("explorer.exe", getConfigsDir()).Start()
	})
}

// ─── Crash guard ──────────────────────────────────────────────────────────────
// Wails bindings run on their own goroutines — an unrecovered panic there kills
// the whole process with no visible output (GUI build). These wrappers recover
// panics, append the full stack to data/crash.log, and turn the panic into a
// normal error the frontend can show.

func crashLogPath() string {
	return filepath.Join(getDataDir(), "crash.log")
}

func appendCrashLog(content string) {
	f, err := os.OpenFile(crashLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[crash] cannot write crash.log:", err)
		return
	}
	defer f.Close()
	f.WriteString(content)
}

func writeCrash(name string, r interface{}) {
	appendCrashLog(fmt.Sprintf("===== %s | [%s] panic: %v =====\n%s\n",
		time.Now().Format("2006-01-02 15:04:05"), name, r, debug.Stack()))
}

// guardP wraps a binding returning a plain value.
func guardP[T any](name string, fn func() T) (result T) {
	defer func() {
		if r := recover(); r != nil {
			writeCrash(name, r)
			var zero T
			result = zero
		}
	}()
	return fn()
}

// guardE wraps a binding returning only an error.
func guardE(name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			writeCrash(name, r)
			err = fmt.Errorf("内部错误: %v (详情见 data/crash.log)", r)
		}
	}()
	return fn()
}

// guardR wraps a binding returning (value, error).
func guardR[T any](name string, fn func() (T, error)) (result T, err error) {
	defer func() {
		if r := recover(); r != nil {
			writeCrash(name, r)
			var zero T
			result, err = zero, fmt.Errorf("内部错误: %v (详情见 data/crash.log)", r)
		}
	}()
	return fn()
}

// ─── Node APIs ───────────────────────────────────────────────────────────────

func (a *App) GetNodes() []node.Node {
	return guardP("GetNodes", func() []node.Node {
		return a.nodeStore.GetAll()
	})
}

func (a *App) ImportNodes(content, groupID string) (int, error) {
	return guardR("ImportNodes", func() (int, error) {
		nodes, err := node.ParseContent(content)
		if err != nil {
			return 0, err
		}
		gid := node.DefaultGroupID
		if a.nodeStore.GroupExists(groupID) {
			gid = groupID
		}
		for i := range nodes {
			nodes[i].GroupID = gid
		}
		a.nodeStore.AddMany(nodes)
		return len(nodes), a.nodeStore.Save()
	})
}

// FetchSubGroupResult 拉取订阅建组的结果（供前端选中新建分组）。
type FetchSubGroupResult struct {
	Group node.Group `json:"group"`
	Count int        `json:"count"`
}

// FetchSubscriptionAsGroup 拉取订阅 URL：成功后新建一个「订阅N」分组
//（追加到分组列表最右侧，名称/订阅链接自动填充，不开启自动更新），
// 拉取的节点全部放入该分组。拉取失败则不做任何改动。
func (a *App) FetchSubscriptionAsGroup(url string) (FetchSubGroupResult, error) {
	return guardR("FetchSubscriptionAsGroup", func() (FetchSubGroupResult, error) {
		url = strings.TrimSpace(url)
		if url == "" {
			return FetchSubGroupResult{}, fmt.Errorf("订阅链接不能为空")
		}
		nodes, err := node.FetchSubscription(url,
			a.cfgManager.Settings.SubUserAgent,
			a.cfgManager.Settings.SubTimeoutSec)
		if err != nil {
			return FetchSubGroupResult{}, err
		}
		// 新建分组（追加到最右侧）并填入订阅链接（不自动更新）
		g, err := a.nodeStore.AddGroup(nextSubGroupName(a.nodeStore.GetGroups()), "")
		if err != nil {
			return FetchSubGroupResult{}, err
		}
		if err := a.nodeStore.UpdateGroup(g.ID, g.Name, url, false, 0); err != nil {
			return FetchSubGroupResult{}, err
		}
		for i := range nodes {
			nodes[i].GroupID = g.ID
			nodes[i].SubURL = url
		}
		a.nodeStore.AddMany(nodes)
		// 同步记录到全局订阅列表（弹窗的「已添加的订阅」）
		a.cfgManager.Settings.Subscriptions = appendUnique(a.cfgManager.Settings.Subscriptions, url)
		a.cfgManager.Save()
		return FetchSubGroupResult{Group: g, Count: len(nodes)}, nil
	})
}

// nextSubGroupName 返回下一个可用的「订阅N」名称（N 取现有最大值 +1，
// 删过中间的订阅组也不会撞名）。
func nextSubGroupName(groups []node.Group) string {
	maxN := 0
	for _, g := range groups {
		var n int
		if _, err := fmt.Sscanf(g.Name, "订阅%d", &n); err == nil && n > maxN {
			maxN = n
		}
	}
	return fmt.Sprintf("订阅%d", maxN+1)
}

func (a *App) ClearNodes() error {
	return guardE("ClearNodes", func() error {
		a.nodeStore.Clear()
		return a.nodeStore.Save()
	})
}

func (a *App) DeleteNode(id string) error {
	return guardE("DeleteNode", func() error {
		a.nodeStore.Delete(id)
		return a.nodeStore.Save()
	})
}

func (a *App) UpdateNode(n node.Node) error {
	return guardE("UpdateNode", func() error {
		a.nodeStore.Update(n)
		return a.nodeStore.Save()
	})
}

// ApplyNode: replace the "proxy" outbound (sing-box) or proxies entry (mihomo)
// in config file with this node.
// 若核心正在运行，则先停核心、改配置、同步 run 目录、再拉起（保证新节点即时生效）
func (a *App) ApplyNode(id string) error {
	return guardE("ApplyNode", func() error {
		s := a.cfgManager.Settings
		core := s.Core
		cfgPath := a.currentConfigPath()
		builtin := config.IsBuiltinMode(s.RoutingMode)
		n := a.nodeStore.Get(id)
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		wasRunning := a.sbProcess.GetStatus().Running
		if wasRunning {
			if err := a.sbProcess.Stop(); err != nil {
				return fmt.Errorf("停止核心失败: %v", err)
			}
		}
		if builtin {
			// 内置模式：只记录节点 ID，配置由模板合成时写入
			a.cfgManager.Settings.AppliedNodeID = n.ID
			if err := a.cfgManager.Save(); err != nil {
				return fmt.Errorf("记录应用节点失败: %v", err)
			}
			if err := a.syncRunConfig(core, ""); err != nil {
				return err
			}
		} else if cfgPath == "" {
			// custom 模式但未选择配置文件：仅记录应用节点，
			// 不写文件（否则与「选择内置配置需要先应用节点」形成死锁）
			a.cfgManager.Settings.AppliedNodeID = n.ID
			if err := a.cfgManager.Save(); err != nil {
				return fmt.Errorf("记录应用节点失败: %v", err)
			}
		} else {
			if err := config.ApplyNodeToConfig(core, cfgPath, *n); err != nil {
				return err
			}
			// 同步到 run 目录（先清除旧配置）
			if err := a.syncRunConfig(core, cfgPath); err != nil {
				return err
			}
			// 持久化应用的节点 ID（切换配置文件后据此重新应用）
			a.cfgManager.Settings.AppliedNodeID = n.ID
			if err := a.cfgManager.Save(); err != nil {
				return fmt.Errorf("记录应用节点失败: %v", err)
			}
		}
		if wasRunning {
			if err := a.startCore(); err != nil {
				return fmt.Errorf("节点已写入配置，但核心重启失败: %v（请手动启动核心）", err)
			}
		}
		return nil
	})
}

// ExportNodeURI converts a node back to its share URI (e.g. vless://...)
func (a *App) ExportNodeURI(id string) (string, error) {
	return guardR("ExportNodeURI", func() (string, error) {
		n := a.nodeStore.Get(id)
		if n == nil {
			return "", fmt.Errorf("节点不存在")
		}
		return node.NodeToURI(*n)
	})
}

// MoveNodeUp moves the node one position up within its group
func (a *App) MoveNodeUp(id string) error {
	return guardE("MoveNodeUp", func() error {
		if !a.nodeStore.Move(id, -1) {
			return fmt.Errorf("已在顶部")
		}
		return a.nodeStore.Save()
	})
}

// MoveNodeDown moves the node one position down within its group
func (a *App) MoveNodeDown(id string) error {
	return guardE("MoveNodeDown", func() error {
		if !a.nodeStore.Move(id, 1) {
			return fmt.Errorf("已在底部")
		}
		return a.nodeStore.Save()
	})
}

// UnapplyNode 取消应用当前节点：
//   - 内置路由模式 / 未选配置文件：仅清除记录的应用节点 ID；
//   - custom 模式：从配置文件移除 "proxy" 出站（mihomo 同步清理 proxy 组引用），
//     否则配置反查会让"已应用"标记立刻回来。
//
// 核心在运行时按「停核心 → 改配置 → 同步 run → 拉起核心」的既有编排执行。
func (a *App) UnapplyNode() error {
	return guardE("UnapplyNode", func() error {
		s := a.cfgManager.Settings
		core := s.Core
		cfgPath := a.currentConfigPath()
		builtin := config.IsBuiltinMode(s.RoutingMode)

		if !builtin && cfgPath != "" {
			wasRunning := a.sbProcess.GetStatus().Running
			if wasRunning {
				if err := a.sbProcess.Stop(); err != nil {
					return fmt.Errorf("停止核心失败: %v", err)
				}
			}
			if err := config.RemoveNodeFromConfig(core, cfgPath); err != nil {
				return err
			}
			if err := a.syncRunConfig(core, cfgPath); err != nil {
				return err
			}
			a.cfgManager.Settings.AppliedNodeID = ""
			if err := a.cfgManager.Save(); err != nil {
				return fmt.Errorf("保存设置失败: %v", err)
			}
			if wasRunning {
				if err := a.startCore(); err != nil {
					return fmt.Errorf("已取消应用，但核心重启失败: %v（请手动启动核心）", err)
				}
			}
			return nil
		}
		// 内置模式 / 未选配置文件：只清标记（内置配置合成必须依赖应用节点，
		// 下次启动核心时会给出明确提示）
		a.cfgManager.Settings.AppliedNodeID = ""
		return a.cfgManager.Save()
	})
}

// TestNodeLatency 测试节点真连接延迟（毫秒）。
// 实现与 v2rayN 一致：用临时 sing-box 实例代理真实 HTTP 请求（generate_204）。
func (a *App) TestNodeLatency(id string) (int, error) {
	return guardR("TestNodeLatency", func() (int, error) {
		n := a.nodeStore.Get(id)
		if n == nil {
			return 0, fmt.Errorf("节点不存在")
		}
		return probe.TestLatency(getCoreBin(config.CoreSingBox), *n)
	})
}

// TestNodeSpeed 测试节点下载速度（Mbps）。
// 实现与 v2rayN 一致：用临时 sing-box 实例经代理下载测速文件，最长 10 秒。
func (a *App) TestNodeSpeed(id string) (float64, error) {
	return guardR("TestNodeSpeed", func() (float64, error) {
		n := a.nodeStore.Get(id)
		if n == nil {
			return 0, fmt.Errorf("节点不存在")
		}
		return probe.TestSpeed(getCoreBin(config.CoreSingBox), *n)
	})
}

// ─── Config file APIs ────────────────────────────────────────────────────────

// GetSettings returns the full settings object.
func (a *App) GetSettings() config.Settings {
	return guardP("GetSettings", func() config.Settings {
		if a.cfgManager == nil {
			return config.Defaults()
		}
		return a.cfgManager.Settings
	})
}

// SaveSettings validates and persists the full settings object.
// 部分设置（日志上限）立即生效；代理/TUN 相关设置在下次开启时生效。
func (a *App) SaveSettings(s config.Settings) error {
	return guardE("SaveSettings", func() error {
		if a.cfgManager == nil {
			return fmt.Errorf("设置尚未就绪，请重启应用")
		}
		s.ProxyListen = strings.TrimSpace(s.ProxyListen)
		s.SubUserAgent = strings.TrimSpace(s.SubUserAgent)
		s.Normalize()
		if err := s.Validate(); err != nil {
			return err
		}
		oldCore := a.cfgManager.Settings.Core
		oldAutoStart := a.cfgManager.Settings.AutoStart
		oldSilentStart := a.cfgManager.Settings.SilentStart
		// tun_enabled 是运行时状态（只能经 EnableTun/DisableTun 改变）：
		// 设置表单快照可能过期（如开 TUN 后再保存设置），不允许覆盖，
		// 否则后端标志被翻回 false，启动核心时既不提权也不再合成 TUN 配置。
		s.TunEnabled = a.cfgManager.Settings.TunEnabled
		a.cfgManager.Settings = s
		// 切换内核：选中路径切换为新内核各自记忆的配置文件（可能为空 = 尚未选择）
		// 内置模式下不回填真实路径，保证前端下拉回显内置项
		if oldCore != s.Core {
			if config.IsBuiltinMode(s.RoutingMode) {
				a.cfgManager.Settings.ConfigPath = ""
			} else {
				a.cfgManager.Settings.ConfigPath = a.cfgManager.Settings.ActiveConfigPath()
			}
		}
		if err := a.cfgManager.Save(); err != nil {
			return fmt.Errorf("保存设置失败: %v", err)
		}
		// 立即生效的设置
		a.sbProcess.SetMaxLog(s.LogMaxLines)
		// 开机自启动/静默启动变化时同步系统自启动项
		if s.AutoStart != oldAutoStart || (s.AutoStart && s.SilentStart != oldSilentStart) {
			if err := a.applyAutoStart(s.AutoStart, s.SilentStart); err != nil {
				return fmt.Errorf("设置已保存，但自启动项更新失败: %v", err)
			}
		}
		return nil
	})
}

// applyAutoStart 注册/移除开机自启动项。
// 参考	v2rayN：以管理员运行时用计划任务（/RL HIGHEST），开机后仍以管理员身份启动；
// 普通权限运行时写注册表 Run 键。两种机制先都清理，避免残留。
func (a *App) applyAutoStart(enable, silent bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	_ = winutil.DeleteRegistryRun()
	_ = winutil.DeleteScheduledTask()
	if !enable {
		return nil
	}
	if winutil.IsAdmin() {
		return winutil.CreateScheduledTask(exe, silent)
	}
	return winutil.SetRegistryRun(exe, silent)
}

// GetAppliedNodeID 找出配置文件中当前应用的节点 ID
//（sing-box：tag "proxy" 的 outbound；mihomo：name "proxy" 的 proxies 条目）。
// 无匹配（含未选配置/手工改配置）返回 ""。
func (a *App) GetAppliedNodeID() string {
	return guardP("GetAppliedNodeID", func() string {
		s := a.cfgManager.Settings
		// 内置模式没有配置文件可反查，直接返回记录的应用节点；
		// custom 模式未选配置文件时同样返回记录值（应用节点不再强制要求先选文件）
		if config.IsBuiltinMode(s.RoutingMode) || s.ActiveConfigPath() == "" {
			return s.AppliedNodeID
		}
		return config.FindAppliedNodeID(s.Core, a.currentConfigPath(), a.nodeStore.GetAll())
	})
}

// The old native file dialog has been replaced by the configs-dir dropdown:
// see GetConfigFiles / SelectConfigFile(name) / OpenConfigsDir.

func (a *App) GetSubscriptions() []string {
	return guardP("GetSubscriptions", func() []string {
		if a.cfgManager == nil {
			return []string{}
		}
		return a.cfgManager.Settings.Subscriptions
	})
}

func (a *App) RemoveSubscription(url string) error {
	return guardE("RemoveSubscription", func() error {
		subs := a.cfgManager.Settings.Subscriptions
		newSubs := []string{}
		for _, s := range subs {
			if s != url {
				newSubs = append(newSubs, s)
			}
		}
		a.cfgManager.Settings.Subscriptions = newSubs
		return a.cfgManager.Save()
	})
}

// ─── Group APIs ────────────────────────────────────────────────────────────────
func (a *App) GetGroups() []node.Group {
	return guardP("GetGroups", func() []node.Group {
		return a.nodeStore.GetGroups()
	})
}

// AddGroup creates a new group right after afterID ("": append at end)
func (a *App) AddGroup(name, afterID string) (node.Group, error) {
	return guardR("AddGroup", func() (node.Group, error) {
		return a.nodeStore.AddGroup(name, afterID)
	})
}

func (a *App) RenameGroup(id, name string) error {
	return guardE("RenameGroup", func() error {
		return a.nodeStore.RenameGroup(id, name)
	})
}

// UpdateGroup 编辑分组：名称 + 订阅链接 + 自动更新设置。
// 默认分组名称固定为「默认」，但订阅设置同样可编辑。
func (a *App) UpdateGroup(id, name, subURL string, autoUpdate bool, intervalHours int) error {
	return guardE("UpdateGroup", func() error {
		return a.nodeStore.UpdateGroup(id, name, subURL, autoUpdate, intervalHours)
	})
}

// MoveGroup 与紧邻分组交换位置（delta: -1 左移 / +1 右移）。
// 默认分组永远保持在最左侧。
func (a *App) MoveGroup(id string, delta int) error {
	return guardE("MoveGroup", func() error {
		return a.nodeStore.MoveGroup(id, delta)
	})
}

// RefreshGroupSubscription 手动更新分组订阅：拉取成功后清空该分组全部节点，
// 写入新拉取的节点。
func (a *App) RefreshGroupSubscription(groupID string) (int, error) {
	return guardR("RefreshGroupSubscription", func() (int, error) {
		return a.refreshGroupSubscription(groupID)
	})
}

// refreshGroupSubscription 分组订阅更新（手动菜单与后台自动更新共用）。
func (a *App) refreshGroupSubscription(groupID string) (int, error) {
	g := a.nodeStore.GroupByID(groupID)
	if g == nil {
		return 0, fmt.Errorf("分组不存在")
	}
	if strings.TrimSpace(g.SubURL) == "" {
		return 0, fmt.Errorf("该分组未设置订阅链接")
	}
	nodes, err := node.FetchSubscription(g.SubURL,
		a.cfgManager.Settings.SubUserAgent,
		a.cfgManager.Settings.SubTimeoutSec)
	if err != nil {
		return 0, err
	}
	// 拉取成功后才清空分组并写入新节点
	a.nodeStore.RemoveByGroup(groupID)
	for i := range nodes {
		nodes[i].GroupID = groupID
		nodes[i].SubURL = g.SubURL
	}
	a.nodeStore.AddMany(nodes)
	a.nodeStore.SetGroupLastUpdate(groupID, time.Now().Unix())
	// 已应用节点若在该分组中被替换掉，清除应用标记
	if s := a.cfgManager.Settings; s.AppliedNodeID != "" && a.nodeStore.Get(s.AppliedNodeID) == nil {
		a.cfgManager.Settings.AppliedNodeID = ""
		a.cfgManager.Save()
	}
	return len(nodes), nil
}

// startAutoUpdateLoop 启动分组订阅自动更新循环。
// 每 10 分钟巡检一次：分组设置了订阅链接且开启自动更新、
// 距上次更新超过设定间隔（小时）时，静默拉取更新。
func (a *App) startAutoUpdateLoop() {
	a.autoStop = make(chan struct{})
	stop := a.autoStop
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				a.autoUpdateGroups()
			}
		}
	}()
}

func (a *App) autoUpdateGroups() {
	if a.cfgManager == nil || a.nodeStore == nil {
		return
	}
	for _, g := range a.nodeStore.GetGroups() {
		if strings.TrimSpace(g.SubURL) == "" || !g.AutoUpdate || g.UpdateIntervalHours <= 0 {
			continue
		}
		if time.Now().Unix()-g.LastUpdate < int64(g.UpdateIntervalHours)*3600 {
			continue
		}
		if _, err := a.refreshGroupSubscription(g.ID); err != nil {
			// 自动更新失败只记录，不打扰用户
			fmt.Println("[auto-update] 分组", g.Name, "自动更新失败:", err)
		}
	}
}

// DeleteGroup removes a group; its nodes move into the 默认 group
func (a *App) DeleteGroup(id string) error {
	return guardE("DeleteGroup", func() error {
		return a.nodeStore.DeleteGroup(id)
	})
}

// ─── TUN APIs ────────────────────────────────────────────────────────────────

func (a *App) EnableTun() error {
	return guardE("EnableTun", func() error {
		s := a.cfgManager.Settings
		core := s.Core
		cfgPath := a.currentConfigPath()
		builtin := config.IsBuiltinMode(s.RoutingMode)
		if cfgPath == "" && !builtin {
			return fmt.Errorf("未选择配置文件")
		}
		// TUN 开关依赖核心状态：核心在跑则先停、改配置、同步 run、再拉起，保证即时生效
		wasRunning := a.sbProcess.GetStatus().Running
		if wasRunning {
			if err := a.sbProcess.Stop(); err != nil {
				return fmt.Errorf("停止核心失败: %v", err)
			}
		}
		// 持久化开关状态（内置模式据此生成 tun inbound）
		s.TunEnabled = true
		a.cfgManager.Settings = s
		if err := a.cfgManager.Save(); err != nil {
			return fmt.Errorf("保存设置失败: %v", err)
		}
		if builtin {
			// 内置模式：TUN 由模板合成，无需改用户配置文件
			if err := a.syncRunConfig(core, ""); err != nil {
				return err
			}
		} else {
			if err := config.SetTun(core, cfgPath, true, s.TunStack, s.TunMTU, s.TunStrictRoute); err != nil {
				return err
			}
			if err := a.syncRunConfig(core, cfgPath); err != nil {
				return err
			}
		}
		if wasRunning {
			if err := a.startCore(); err != nil {
				return fmt.Errorf("TUN 已写入配置，但核心启动失败: %v（请手动启动核心）", err)
			}
		}
		return nil
	})
}

func (a *App) DisableTun() error {
	return guardE("DisableTun", func() error {
		s := a.cfgManager.Settings
		core := s.Core
		cfgPath := a.currentConfigPath()
		builtin := config.IsBuiltinMode(s.RoutingMode)
		if cfgPath == "" && !builtin {
			return fmt.Errorf("未选择配置文件")
		}
		wasRunning := a.sbProcess.GetStatus().Running
		if wasRunning {
			if err := a.sbProcess.Stop(); err != nil {
				return fmt.Errorf("停止核心失败: %v", err)
			}
		}
		s.TunEnabled = false
		a.cfgManager.Settings = s
		if err := a.cfgManager.Save(); err != nil {
			return fmt.Errorf("保存设置失败: %v", err)
		}
		if builtin {
			if err := a.syncRunConfig(core, ""); err != nil {
				return err
			}
		} else {
			if err := config.SetTun(core, cfgPath, false, "", 0, false); err != nil {
				return err
			}
			if err := a.syncRunConfig(core, cfgPath); err != nil {
				return err
			}
		}
		if wasRunning {
			if err := a.startCore(); err != nil {
				return fmt.Errorf("TUN 已移除，但核心启动失败: %v（请手动启动核心）", err)
			}
		}
		return nil
	})
}

// ─── System Proxy APIs ───────────────────────────────────────────────────────

func (a *App) EnableSystemProxy() error {
	return guardE("EnableSystemProxy", func() error {
		s := a.cfgManager.Settings
		core := s.Core
		cfgPath := a.currentConfigPath()
		builtin := config.IsBuiltinMode(s.RoutingMode)
		if cfgPath == "" && !builtin {
			return fmt.Errorf("未选择配置文件")
		}
		// 核心在跑则先停、改配置、同步 run、再拉起，保证 mixed inbound / mixed-port 即时生效
		wasRunning := a.sbProcess.GetStatus().Running
		if wasRunning {
			if err := a.sbProcess.Stop(); err != nil {
				return fmt.Errorf("停止核心失败: %v", err)
			}
		}
		if builtin {
			// 内置模式：先设注册表再合成（合成时按注册表状态写入 mixed-port）
			if err := a.proxy.Enable("127.0.0.1", s.ProxyPort); err != nil {
				return err
			}
			if err := a.syncRunConfig(core, ""); err != nil {
				return err
			}
		} else {
			if err := config.SetMixedInbound(core, cfgPath, true, s.ProxyListen, s.ProxyPort); err != nil {
				return err
			}
			if err := a.syncRunConfig(core, cfgPath); err != nil {
				return err
			}
		}
		if wasRunning {
			if err := a.startCore(); err != nil {
				return fmt.Errorf("系统代理已写入配置，但核心启动失败: %v（请手动启动核心）", err)
			}
		}
		// Windows 系统代理地址固定为 127.0.0.1（监听地址可以是 0.0.0.0/::，但注册表里不能）
		if !builtin {
			return a.proxy.Enable("127.0.0.1", s.ProxyPort)
		}
		return nil
	})
}

func (a *App) DisableSystemProxy() error {
	return guardE("DisableSystemProxy", func() error {
		s := a.cfgManager.Settings
		builtin := config.IsBuiltinMode(s.RoutingMode)
		if builtin {
			// 内置模式：关注册表后重新合成，移除配置中的 mixed-port
			// （核心未运行时只关注册表，run 配置由下次 startCore 重新合成）
			if err := a.proxy.Disable(); err != nil {
				return err
			}
			wasRunning := a.sbProcess.GetStatus().Running
			if wasRunning {
				if err := a.sbProcess.Stop(); err != nil {
					return fmt.Errorf("停止核心失败: %v", err)
				}
				if err := a.syncRunConfig(s.Core, ""); err != nil {
					return err
				}
				if err := a.startCore(); err != nil {
					return fmt.Errorf("系统代理已关闭，但核心启动失败: %v（请手动启动核心）", err)
				}
			}
			return nil
		}
		return a.proxy.Disable()
	})
}

// ─── Core process APIs ─────────────────────────────────────────────────────

// StartSingBox 启动当前内核（sing-box 或 mihomo）。
// 启动前把选中的配置文件同步到 run 目录（不存在则视为首次同步），
// sing-box 使用 run -D run，mihomo 使用 -d run。
func (a *App) StartSingBox() error {
	return guardE("StartSingBox", func() error {
		return a.startCore()
	})
}

func (a *App) StopSingBox() error {
	return guardE("StopSingBox", func() error {
		return a.sbProcess.Stop()
	})
}

func (a *App) GetSingBoxStatus() singbox.Status {
	return guardP("GetSingBoxStatus", func() singbox.Status {
		return a.sbProcess.GetStatus()
	})
}

func (a *App) GetSingBoxLog() []string {
	return guardP("GetSingBoxLog", func() []string {
		return a.sbProcess.GetLog()
	})
}

// getCoreBin 返回指定内核的二进制路径（程序目录 bin/ 下）。
func getCoreBin(core string) string {
	name := "sing-box.exe"
	if core == config.CoreMihomo {
		name = "mihomo.exe"
	}
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("bin", name)
	}
	return filepath.Join(filepath.Dir(exe), "bin", name)
}

// startCore 用 run 目录中的配置启动当前内核。
// 启动前把选中的源配置重新同步到 run 目录（同时覆盖"run 配置不存在"的情况），
// 源文件已包含应用的节点/TUN/系统代理设置。
func (a *App) startCore() error {
	s := a.cfgManager.Settings
	core := s.Core
	// 内置模式没有用户配置文件，配置由模板合成
	if s.ActiveConfigPath() == "" && !config.IsBuiltinMode(s.RoutingMode) {
		return fmt.Errorf("未选择配置文件")
	}
	// TUN 模式必须管理员权限（tun inbound 建网卡需要）：非管理员运行时
	// 申请 UAC 提权并重启程序，重启后自动恢复核心运行（参考 v2rayN）。
	// 管理员身份运行时核心子进程自动继承管理员令牌，无需特殊处理。
	if s.TunEnabled && !winutil.IsAdmin() {
		return a.requestElevationAndRestart(true)
	}
	binPath := getCoreBin(core)
	if _, err := os.Stat(binPath); err != nil {
		if core == config.CoreMihomo {
			return fmt.Errorf("未找到 mihomo 内核: %s（请将 mihomo.exe 放入 bin 目录）", binPath)
		}
		return fmt.Errorf("未找到 sing-box 内核: %s（请将 sing-box.exe 放入 bin 目录）", binPath)
	}
	if err := a.syncRunConfig(core, a.currentConfigPath()); err != nil {
		return err
	}
	ensureRunDir()
	var args []string
	if core == config.CoreMihomo {
		// mihomo：-d 指定工作目录（配置与 geodata 所在地），-f 指定配置文件
		args = []string{"-d", getRunDir(), "-f", runConfigPath(core)}
	} else {
		// sing-box：-D 指定工作目录，-c 指定配置文件
		args = []string{"run", "-D", getRunDir(), "-c", runConfigPath(core)}
	}
	return a.sbProcess.Start(binPath, args, core)
}

// ─── 提权重启（TUN 模式需要管理员权限）────────────────────────────────────────

// elevateMarker 记录提权重启后需要恢复的状态（data/elevate.json，一次性）。
type elevateMarker struct {
	StartCore bool `json:"start_core"` // 重启后自动拉起核心（TUN 开启状态下提权）
}

// requestElevationAndRestart 触发 UAC 授权并以管理员身份重启程序：
// 写恢复标记 → ShellExecute runas 启动新实例 → 旧实例停核心释放端口后退出。
// 新实例启动时读取标记恢复原有状态（restoreAfterElevation）。
func (a *App) requestElevationAndRestart(startCoreAfter bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %v", err)
	}
	markerPath := filepath.Join(getDataDir(), "elevate.json")
	data, _ := json.Marshal(elevateMarker{StartCore: startCoreAfter})
	if err := os.WriteFile(markerPath, data, 0644); err != nil {
		return fmt.Errorf("写入提权标记失败: %v", err)
	}
	if err := winutil.LaunchElevated(exe, ""); err != nil {
		os.Remove(markerPath) // 用户取消 UAC：清理标记，原地不动
		return fmt.Errorf("需要管理员权限（TUN 模式），但授权被取消: %v", err)
	}
	// 新实例即将拉起：先停掉旧核心释放端口/网卡，再退出当前（非管理员）实例
	a.sbProcess.Stop()
	runtime.Quit(a.ctx)
	return fmt.Errorf("正在以管理员身份重启程序…")
}

// restoreAfterElevation 提权重启后的新实例：消费恢复标记并恢复原有状态。
// 在 startup 的 goroutine 中执行。
func (a *App) restoreAfterElevation() {
	markerPath := filepath.Join(getDataDir(), "elevate.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return // 非提权重启，正常启动
	}
	_ = os.Remove(markerPath)
	var m elevateMarker
	if json.Unmarshal(data, &m) != nil || !m.StartCore {
		return
	}
	s := a.cfgManager.Settings
	if !s.TunEnabled {
		return
	}
	// 等旧实例完全退出、释放 TUN 网卡与端口后拉起核心（最多等 15 秒）
	for i := 0; i < 15; i++ {
		time.Sleep(time.Second)
		if err := a.startCore(); err == nil {
			return
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

func (a *App) ShowMessage(title, msg string) {
	defer func() {
		if r := recover(); r != nil {
			writeCrash("ShowMessage", r)
		}
	}()
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: msg,
	})
}

// GetConfigPreview returns the current config content for display.
// sing-box JSON 格式化输出；mihomo YAML 原样输出。
func (a *App) GetConfigPreview() (string, error) {
	return guardR("GetConfigPreview", func() (string, error) {
		var data []byte
		var ext string
		if config.IsBuiltinMode(a.cfgManager.Settings.RoutingMode) {
			// 内置模式显示合成到 run 目录的最终配置
			core := a.cfgManager.Settings.Core
			p := runConfigPath(core)
			d, err := os.ReadFile(p)
			if err != nil {
				return "", fmt.Errorf("尚未生成内置配置（请先启动核心）")
			}
			data, ext = d, filepath.Ext(p)
		} else {
			cfgPath := a.currentConfigPath()
			if cfgPath == "" {
				return "", fmt.Errorf("未选择配置文件")
			}
			d, err := os.ReadFile(cfgPath)
			if err != nil {
				return "", err
			}
			data, ext = d, strings.ToLower(filepath.Ext(cfgPath))
		}
		if ext == ".yaml" || ext == ".yml" {
			return string(data), nil
		}
		var obj interface{}
		if err := json.Unmarshal(data, &obj); err != nil {
			return "", err
		}
		pretty, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return "", err
		}
		return string(pretty), nil
	})
}
