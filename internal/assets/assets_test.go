package assets_test

import (
	"testing"

	"github.com/ADWMC/helm-x/internal/assets"
)

func TestPromptModes(t *testing.T) {
	modes := assets.PromptModes()
	if len(modes) != 3 {
		t.Fatalf("expected exactly 3 prompt modes, got %d", len(modes))
	}

	expected := []struct {
		id    string
		isDef bool
	}{
		{"v2.1", true},
		{"v2", false},
		{"ctf", false},
	}

	for i, exp := range expected {
		if modes[i].ID != exp.id {
			t.Errorf("mode[%d].ID = %q, want %q", i, modes[i].ID, exp.id)
		}
		if modes[i].Default != exp.isDef {
			t.Errorf("mode[%d].Default = %v, want %v", i, modes[i].Default, exp.isDef)
		}
		if modes[i].Bytes <= 0 {
			t.Errorf("mode[%d].Bytes = %d, want > 0", i, modes[i].Bytes)
		}
	}
}

func TestPromptFallback(t *testing.T) {
	v21 := assets.Prompt("v2.1")
	if v21 == "" {
		t.Fatal("Prompt(v2.1) is empty")
	}

	v2 := assets.Prompt("v2")
	if v2 == "" {
		t.Fatal("Prompt(v2) is empty")
	}

	ctf := assets.Prompt("ctf")
	if ctf == "" {
		t.Fatal("Prompt(ctf) is empty")
	}

	// Unknown or default should fallback to v2.1
	if got := assets.Prompt("unknown"); got != v21 {
		t.Errorf("Prompt(unknown) did not fallback to v2.1")
	}
	if got := assets.Prompt("default"); got != v21 {
		t.Errorf("Prompt(default) did not fallback to v2.1")
	}
}

func TestIsValidPromptMode(t *testing.T) {
	valid := []string{"v2.1", "v2", "ctf"}
	for _, id := range valid {
		if !assets.IsValidPromptMode(id) {
			t.Errorf("IsValidPromptMode(%q) = false, want true", id)
		}
	}

	invalid := []string{"", "default", "v45", "deepseek", "fusion", "lite", "unknown"}
	for _, id := range invalid {
		if assets.IsValidPromptMode(id) {
			t.Errorf("IsValidPromptMode(%q) = true, want false", id)
		}
	}
}

func TestXORRoundTripFidelity(t *testing.T) {
	files := assets.List()
	if len(files) == 0 {
		t.Fatal("List() returned no files")
	}

	for _, rel := range files {
		content := assets.Get(rel)
		if content == "" {
			t.Errorf("assets.Get(%q) returned empty string", rel)
		}
		// Summary check
		if len(content) < 10 {
			t.Errorf("assets.Get(%q) returned suspiciously short content (%d bytes)", rel, len(content))
		}
	}

	// 验证核心规则和提示词均能完整解混淆
	tamper := assets.TamperRules()
	if len(tamper) == 0 {
		t.Error("TamperRules() returned empty string")
	}

	rewrite := assets.RewritePrompt()
	if len(rewrite) == 0 {
		t.Error("RewritePrompt() returned empty string")
	}

	qa := assets.QA()
	if qa.Version == 0 || len(qa.Items) == 0 {
		t.Errorf("QA() parsed incorrectly: version=%d items=%d", qa.Version, len(qa.Items))
	}
}
