//go:build fusion && darwin

package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFusionDoesNotReadSharedKeychain(t *testing.T) {
	r := t.TempDir()
	t.Setenv("HOME", r)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(r, "claude"))
	bin := filepath.Join(r, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(r, "keychain-access")
	t.Setenv("FUSION_KEYCHAIN_MARKER", marker)
	program := "#!/usr/bin/python3\nimport os\nopen(os.environ['FUSION_KEYCHAIN_MARKER'],'a').write('called\\n')\nprint('{\"claudeAiOauth\":{\"accessToken\":\"fixture-only-token\",\"refreshToken\":\"fixture-refresh\",\"expiresAt\":9999999999999}}')\n"
	if err := os.WriteFile(filepath.Join(bin, "security"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	if _, _, ok := readClaudeCredential(); ok {
		t.Error("shared Claude credential accepted")
	}
	if _, ok := readClaudeDir(filepath.Join(r, "saved-claude")); ok {
		t.Error("shared saved-account keychain read")
	}
	if token, err := cursorToken(); err == nil || token != "" {
		t.Error("shared Cursor credential accepted")
	}
	if token := copilotCLISecret("fixture-account"); token != "" {
		t.Error("shared Copilot credential accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("Fusion accessed shared keychain")
	}
}
