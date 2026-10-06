package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ADWMC/helm-x/internal/config"
)

func TestDefaultSettings(t *testing.T) {
	d := config.Default()
	if d.PromptMode != "v2.1" {
		t.Errorf("Default().PromptMode = %q, want %q", d.PromptMode, "v2.1")
	}
}

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
	if s.PromptMode != "v2.1" {
		t.Errorf("store.Get().PromptMode = %q, want normalized %q", s.PromptMode, "v2.1")
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
