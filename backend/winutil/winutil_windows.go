//go:build windows

// Package winutil 封装 Windows 相关的系统能力：
//   - 管理员（elevated）检测与提权重启（ShellExecute runas，参考 v2rayN）；
//   - 开机自启动：普通权限写注册表 Run 键；管理员权限用计划任务
//     （schtasks /RL HIGHEST，保证开机后仍以管理员身份运行）。
package winutil

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW = user32.NewProc("FindWindowW")
)

// WaitForTaskbar 等待任务栏窗口（Shell_TrayWnd，由 explorer 创建）就绪。
// 开机自启动的程序经常比 explorer 先启动，此时 Shell_NotifyIcon 无法注册
// 托盘图标（表现为图标空白且点击无响应），必须等任务栏就绪后再注册。
// 返回是否在超时前等到任务栏。
func WaitForTaskbar(timeout time.Duration) bool {
	cls, err := windows.UTF16PtrFromString("Shell_TrayWnd")
	if err != nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		hwnd, _, _ := procFindWindowW.Call(
			uintptr(unsafe.Pointer(cls)),
			0,
		)
		if hwnd != 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// CurrentThreadID 返回当前线程 ID（托盘消息循环所在的锁定线程）。
func CurrentThreadID() uint32 {
	return windows.GetCurrentThreadId()
}

// PostThreadQuit 向指定线程投递 WM_QUIT，让阻塞在 GetMessage 的
// 消息循环退出（用于托盘 ready 超时后的重试）。
func PostThreadQuit(threadID uint32) error {
	const WM_QUIT = 0x0012
	procPostThreadMessage := user32.NewProc("PostThreadMessageW")
	res, _, err := procPostThreadMessage.Call(
		uintptr(threadID),
		WM_QUIT,
		0,
		0,
	)
	if res == 0 {
		return err
	}
	return nil
}

// IsAdmin 报告当前进程是否以管理员（UAC 提权）身份运行。
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// LaunchElevated 以管理员身份启动 exe（触发 UAC 授权框）。
// 用户取消授权时返回 error。
func LaunchElevated(exe, args string) error {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	var argsPtr *uint16
	if args != "" {
		if argsPtr, err = windows.UTF16PtrFromString(args); err != nil {
			return err
		}
	}
	if err := windows.ShellExecute(0, verb, file, argsPtr, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("提权启动失败: %v", err)
	}
	return nil
}

// ─── 开机自启动 ───────────────────────────────────────────────────────────────

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	taskName   = "SM GUI"
	silentArg  = "--silent"
)

// SetRegistryRun 写入当前用户注册表 Run 自启动项（普通权限路径）。
func SetRegistryRun(exe string, silent bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开注册表 Run 键失败: %v", err)
	}
	defer key.Close()
	return key.SetStringValue(taskName, `"`+exe+`"`+silentSuffix(silent))
}

// DeleteRegistryRun 删除注册表 Run 自启动项（不存在视为成功）。
func DeleteRegistryRun() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return nil // 键不存在等，视为已删除
	}
	defer key.Close()
	// 值不存在时返回注册表错误，一律视为已删除
	_ = key.DeleteValue(taskName)
	return nil
}

// CreateScheduledTask 用计划任务注册开机自启动（/RL HIGHEST，
// 开机后以管理员最高权限运行——参考 v2rayN 对管理员自启动的做法）。
func CreateScheduledTask(exe string, silent bool) error {
	action := `"` + exe + `"` + silentSuffix(silent)
	args := []string{"/Create", "/F", "/TN", taskName, "/SC", "ONLOGON", "/RL", "HIGHEST", "/TR", action}
	cmd := exec.Command("schtasks", args...)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建计划任务失败: %v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteScheduledTask 删除自启动计划任务（不存在视为成功）。
func DeleteScheduledTask() error {
	cmd := exec.Command("schtasks", "/Delete", "/F", "/TN", taskName)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		// 任务不存在时 schtasks 返回非 0，视为成功
		if strings.Contains(string(out), "does not exist") || strings.Contains(string(out), "不存在") {
			return nil
		}
		return fmt.Errorf("删除计划任务失败: %v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func silentSuffix(silent bool) string {
	if silent {
		return " " + silentArg
	}
	return ""
}

// hideWindow 阻止 schtasks 弹出控制台窗口（同 singbox 包做法）。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
