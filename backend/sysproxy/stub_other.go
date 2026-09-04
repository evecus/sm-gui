//go:build !windows

package sysproxy

import "fmt"

type Manager struct{}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) Enable(host string, port int) error {
	return fmt.Errorf("系统代理仅支持 Windows")
}

func (m *Manager) IsEnabled() bool {
	return false
}

func (m *Manager) Disable() error {
	return fmt.Errorf("系统代理仅支持 Windows")
}
