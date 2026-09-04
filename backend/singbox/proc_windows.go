//go:build windows

package singbox

import (
	"os/exec"
	"syscall"
)

// hideWindow 给子进程设置 CREATE_NO_WINDOW 标志，
// 阻止 Windows 为子进程分配/弹出新的控制台窗口。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
