//go:build !windows

package singbox

import "os/exec"

// hideWindow 在非 Windows 平台上无需做任何事。
func hideWindow(cmd *exec.Cmd) {}
