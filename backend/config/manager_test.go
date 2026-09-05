package config

import (
	"os"
	"path/filepath"
	"testing"
)

// settings.json 只存 configs 目录下的文件名；builtin 段缺省补默认；
// clash_api_disabled 反转语义：缺省 false = 启用，显式关闭后往返保持。
func TestSettingsPathAndBuiltinDefaults(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "core": "sing-box",
  "config_path": "",
  "config_path_singbox": "config.json",
  "config_path_mihomo": "white.yaml",
  "routing_mode": "custom"
}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(filepath.Join(dir, "settings.json"))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	s := m.Settings
	if s.ConfigPathSingBox != "config.json" || s.ConfigPathMihomo != "white.yaml" {
		t.Fatalf("路径应保持文件名: %q / %q", s.ConfigPathSingBox, s.ConfigPathMihomo)
	}
	// builtin 缺段 → 全默认（clash-api 默认启用）
	if s.Builtin.LogLevel != "warning" || s.Builtin.ClashAPIDisabled != false {
		t.Fatalf("builtin 默认值错误: %+v", s.Builtin)
	}
	// 保存/重载往返
	s.SetCoreConfigPath(CoreSingBox, "config.json")
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(filepath.Join(dir, "settings.json"))
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if m2.Settings.ConfigPathSingBox != "config.json" {
		t.Fatalf("重新加载后 config_path_singbox = %q", m2.Settings.ConfigPathSingBox)
	}
	if m2.Settings.Builtin.ClashAPIDisabled != false {
		t.Fatal("缺省 clash_api_disabled 应为 false（启用）")
	}
	// 显式关闭后往返保持关闭
	m2.Settings.Builtin.ClashAPIDisabled = true
	if err := m2.Save(); err != nil {
		t.Fatal(err)
	}
	m3 := NewManager(filepath.Join(dir, "settings.json"))
	if err := m3.Load(); err != nil {
		t.Fatal(err)
	}
	if m3.Settings.Builtin.ClashAPIDisabled != true {
		t.Fatal("显式关闭的 clash_api_disabled 重载后应保持 true")
	}
}
