package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 旧版 settings.json（绝对路径）加载后应归一化为 configs 目录下的文件名，
// 保证用户移动程序目录后设置仍然有效。
func TestSettingsPathNormalization(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "core": "sing-box",
  "config_path": "",
  "config_path_singbox": "D:\\1tmp\\sm-gui\\configs\\config.json",
  "config_path_mihomo": "D:\\1tmp\\sm-gui\\configs/white.yaml",
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
	if s.ConfigPathSingBox != "config.json" {
		t.Fatalf("config_path_singbox = %q, want config.json", s.ConfigPathSingBox)
	}
	if s.ConfigPathMihomo != "white.yaml" {
		t.Fatalf("config_path_mihomo = %q, want white.yaml", s.ConfigPathMihomo)
	}
	if filepath.IsAbs(s.ConfigPath) || len(s.ConfigPath) > 0 && (s.ConfigPath[0] == '\\' || s.ConfigPath[0] == '/') {
		t.Fatalf("config_path 不应是绝对路径: %q", s.ConfigPath)
	}

	// 新格式（纯文件名）往返保存后不变
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
}
