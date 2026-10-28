package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/config"
)

func TestDefaultSettings(t *testing.T) {
	d := config.Default()
	if d.PromptMode != "ctf-v2.1" {
		t.Errorf("Default().PromptMode = %q, want %q", d.PromptMode, "ctf-v2.1")
	}
}

// 旧版把内置提示词存成 "default"（当时内置的是 2.5KB CTF 计分版）。
// 归一化必须映射到等价的 ctf，而不是静默换成 v2.1 —— 否则升级即换行为。
func TestNormalizeLegacyDefaultPromptMode(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "helmx.config.json")

	// 模拟写入旧版含有 prompt_mode: "default" 的配置
	content := []byte(`{"listen_port": 1800, "prompt_mode": "default"}`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	store := config.NewStore(cfgPath)
	s := store.Get()
	if s.PromptMode != "ctf" {
		t.Errorf("store.Get().PromptMode = %q, want legacy-compatible %q", s.PromptMode, "ctf")
	}
}

// 现行 ID 与历史 ID 双向兼容：读旧值要归一化，写新值要原样保留。
func TestCanonicalPromptModeIDs(t *testing.T) {
	cases := map[string]string{
		"v2":       "ctf-v2",
		"v2.1":     "ctf-v2.1",
		"ctf-v2.1": "ctf-v2.1",
		"ctf":      "ctf",
		"unknown":  "unknown",
	}
	for in, want := range cases {
		if got := assets.CanonicalPromptMode(in); got != want {
			t.Errorf("CanonicalPromptMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// 空值仍然落到当前默认版。
func TestNormalizeEmptyPromptMode(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "helmx.config.json")

	content := []byte(`{"listen_port": 1800}`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	store := config.NewStore(cfgPath)
	if got := store.Get().PromptMode; got != "ctf-v2.1" {
		t.Errorf("store.Get().PromptMode = %q, want %q", got, "ctf-v2.1")
	}
}

func TestPreserveExplicitPromptModes(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "helmx.config.json")

	content := []byte(`{"listen_port": 1800, "prompt_mode": "ctf"}`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	store := config.NewStore(cfgPath)
	s := store.Get()
	if s.PromptMode != "ctf" {
		t.Errorf("store.Get().PromptMode = %q, want %q", s.PromptMode, "ctf")
	}
}
