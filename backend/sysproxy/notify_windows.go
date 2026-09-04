//go:build windows

package sysproxy

import (
	"syscall"
	"unsafe"
)

var (
	wininet            = syscall.NewLazyDLL("wininet.dll")
	internetSetOption  = wininet.NewProc("InternetSetOptionW")
)

const (
	INTERNET_OPTION_SETTINGS_CHANGED = 39
	INTERNET_OPTION_REFRESH          = 37
)

func notifyProxyChange() {
	internetSetOption.Call(0, INTERNET_OPTION_SETTINGS_CHANGED, uintptr(unsafe.Pointer(nil)), 0)
	internetSetOption.Call(0, INTERNET_OPTION_REFRESH, uintptr(unsafe.Pointer(nil)), 0)
}
