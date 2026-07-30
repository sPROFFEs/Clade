package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
	"git.jtsec.local/lab/PrAImate/internal/store"
)

func TestMigrateLegacyLocalLLMAPIKeyMovesSecretIntoEncryptedDB(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praimate")
	t.Setenv("PRAIMATE_HOME", root)
	cfg := &launcher.Config{
		DefaultLocalEndpoint: "https://llm.example",
		DefaultLocalAPIKey:   "legacy-plaintext-secret",
	}
	if err := launcher.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	st, err := store.InitializeWithPassword(filepath.Join(root, "db.sqlite"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, err := core.New(core.Options{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyLocalLLMAPIKey(c, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := loadLocalLLMAPIKey(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-plaintext-secret" {
		t.Fatalf("encrypted setting = %q", got)
	}
	_, configPath, _ := launcher.ConfigPaths()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "legacy-plaintext-secret") {
		t.Fatalf("plaintext config retained API key:\n%s", raw)
	}
	if rawSetting, err := c.GetSetting(context.Background(), core.ScopeCLI, localLLMAPIKeySetting); err != nil || rawSetting == nil {
		t.Fatalf("encrypted setting missing: raw=%s err=%v", rawSetting, err)
	}
}
