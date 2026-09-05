//go:build windows

package probe

import (
	"os/exec"
	"syscall"
)

// hideWindow 给子进程设置 CREATE_NO_WINDOW 标志（同 singbox 包）。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
