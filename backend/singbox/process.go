package singbox

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

type Status struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid"`
	Error   string `json:"error,omitempty"`
}

type Process struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	exited chan struct{} // 当前 cmd 被回收时关闭，用于 Stop 等待进程真正退出
	label string        // 内核名称（sing-box / mihomo），仅用于日志显示
	status Status
	log    []string
	maxLog int
}

func NewProcess(maxLog int) *Process {
	if maxLog <= 0 {
		maxLog = 500
	}
	return &Process{maxLog: maxLog}
}

// SetMaxLog 更新日志保留行数并按需裁剪已有日志。
func (p *Process) SetMaxLog(maxLog int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if maxLog <= 0 {
		maxLog = 500
	}
	p.maxLog = maxLog
	if len(p.log) > p.maxLog {
		p.log = p.log[len(p.log)-p.maxLog:]
	}
}

// Start 启动内核进程。
// binPath 为内核二进制路径；args 为完整启动参数（含配置文件路径），
// 由调用方按内核构造：
//   - sing-box: run -D <run目录> -c <run目录>\config.json
//   - mihomo:   -d <run目录> -f <run目录>\config.yaml
// label 仅用于日志显示（如 "sing-box" / "mihomo"）。
func (p *Process) Start(binPath string, args []string, label string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.cmd.Process != nil {
		return fmt.Errorf("核心已在运行")
	}
	if label == "" {
		label = "核心"
	}
	p.label = label

	p.cmd = exec.Command(binPath, args...)
	hideWindow(p.cmd)
	p.log = []string{}
	p.exited = make(chan struct{})

	// capture stdout+stderr
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := p.cmd.Start(); err != nil {
		p.cmd = nil
		return fmt.Errorf("启动失败: %v", err)
	}

	p.status = Status{Running: true, PID: p.cmd.Process.Pid}
	p.appendLog(fmt.Sprintf("[%s] %s 已启动 PID=%d 参数=%v", now(), label, p.cmd.Process.Pid, args))

	cmd := p.cmd
	exited := p.exited

	// read logs
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			p.appendLog(scanner.Text())
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			p.appendLog(scanner.Text())
		}
	}()

	// watch process：仅当 p.cmd 仍是自己时才更新状态，
	// 防止旧进程的 watcher 在 Stop→Start 快速重启后覆盖新进程状态
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.cmd == cmd {
			if err != nil {
				p.appendLog(fmt.Sprintf("[%s] %s 退出: %v", now(), p.label, err))
				p.status = Status{Running: false, Error: err.Error()}
			} else {
				p.appendLog(fmt.Sprintf("[%s] %s 正常退出", now(), p.label))
				p.status = Status{Running: false}
			}
			p.cmd = nil
		}
		close(exited)
	}()

	return nil
}

func (p *Process) Stop() error {
	p.mu.Lock()
	if p.cmd == nil || p.cmd.Process == nil {
		p.status = Status{Running: false}
		p.mu.Unlock()
		return nil
	}
	exited := p.exited
	err := p.cmd.Process.Kill()
	p.cmd = nil
	p.status = Status{Running: false}
	p.mu.Unlock()

	if err != nil {
		return fmt.Errorf("停止失败: %v", err)
	}

	// 等待进程真正退出并被 watcher 回收：
	// ① 保证 Stop 返回后端口/TUN 已释放，紧接的 Start 不会绑端口失败；
	// ② 保证旧 watcher 已完成，状态不会被延迟覆盖。
	if exited != nil {
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			return fmt.Errorf("等待核心退出超时，进程可能仍在运行")
		}
	}

	p.mu.Lock()
	p.appendLog(fmt.Sprintf("[%s] %s 已停止", now(), p.label))
	p.mu.Unlock()
	return nil
}

func (p *Process) GetStatus() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Process) GetLog() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]string, len(p.log))
	copy(result, p.log)
	return result
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func (p *Process) appendLog(line string) {
	line = stripANSI(line)
	p.log = append(p.log, line)
	if len(p.log) > p.maxLog {
		p.log = p.log[len(p.log)-p.maxLog:]
	}
}

func now() string {
	return time.Now().Format("15:04:05")
}
