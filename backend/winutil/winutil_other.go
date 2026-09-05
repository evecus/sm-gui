//go:build !windows

package winutil

import (
	"fmt"
	"time"
)

// 非 Windows 平台的占位实现（当前程序仅面向 Windows 发布）。

func IsAdmin() bool { return true }

func CurrentThreadID() uint32 { return 0 }

func PostThreadQuit(threadID uint32) error { return nil }

func LaunchElevated(exe, args string) error {
	return errNotSupported()
}

func WaitForTaskbar(timeout time.Duration) bool { return true }

func SetRegistryRun(exe string, silent bool) error      { return errNotSupported() }
func DeleteRegistryRun() error                          { return nil }
func CreateScheduledTask(exe string, silent bool) error { return errNotSupported() }
func DeleteScheduledTask() error                        { return nil }

func errNotSupported() error {
	return fmt.Errorf("仅支持 Windows")
}
