//go:build windows

package sysproxy

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	regPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

// bypass list: common LAN/loopback/internal subnets
var defaultBypass = strings.Join([]string{
	"localhost",
	"127.*",
	"10.*",
	"172.16.*",
	"172.17.*",
	"172.18.*",
	"172.19.*",
	"172.20.*",
	"172.21.*",
	"172.22.*",
	"172.23.*",
	"172.24.*",
	"172.25.*",
	"172.26.*",
	"172.27.*",
	"172.28.*",
	"172.29.*",
	"172.30.*",
	"172.31.*",
	"192.168.*",
	"<local>",
}, ";")

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Enable(host string, port int) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开注册表失败: %v", err)
	}
	defer k.Close()

	proxyServer := fmt.Sprintf("%s:%d", host, port)

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", proxyServer); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", defaultBypass); err != nil {
		return err
	}

	// notify system of proxy change
	notifyProxyChange()
	return nil
}

// IsEnabled 读取注册表判断系统代理当前是否开启。
func (m *Manager) IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("ProxyEnable")
	return err == nil && v != 0
}

func (m *Manager) Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开注册表失败: %v", err)
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return err
	}

	notifyProxyChange()
	return nil
}
