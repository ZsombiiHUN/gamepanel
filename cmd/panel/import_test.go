package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportSourceFromDotEnv(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".env")
	env := `# Pterodactyl
APP_KEY=base64:7NZw/Mk/56iKLVPFAFqc9/IYSZGf/5uw2tSAlfZ7/Gg=
DB_CONNECTION=mysql
DB_HOST=db.internal
DB_PORT=3307
DB_DATABASE=panel
DB_USERNAME=pterodactyl
DB_PASSWORD="p#ss word"
export MAIL_FROM=x@y.z # a comment
`
	if err := os.WriteFile(path, []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, key, err := importSource(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "db.internal:3307" || cfg.User != "pterodactyl" || cfg.Passwd != "p#ss word" || cfg.DBName != "panel" {
		t.Errorf("config: %+v", cfg)
	}
	if key != "base64:7NZw/Mk/56iKLVPFAFqc9/IYSZGf/5uw2tSAlfZ7/Gg=" {
		t.Errorf("key %q", key)
	}
	// A flag wins over the file.
	if _, key, _ := importSource(path, "", "base64:other"); key != "base64:other" {
		t.Errorf("--app-key did not win: %q", key)
	}
	vals, _ := readDotEnv(path)
	if vals["MAIL_FROM"] != "x@y.z" {
		t.Errorf("export/comment handling: %q", vals["MAIL_FROM"])
	}
}

func TestImportSourceErrors(t *testing.T) {
	t.Parallel()
	if _, _, err := importSource("", "", ""); err == nil {
		t.Error("no source accepted")
	}
	if _, _, err := importSource("", "not a dsn", ""); err == nil {
		t.Error("bad DSN accepted")
	}
	path := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(path, []byte("DB_CONNECTION=pgsql\n"), 0o600)
	if _, _, err := importSource(path, "", ""); err == nil {
		t.Error("Postgres accepted")
	}
	cfg, _, err := importSource("", "u:p@tcp(h:3306)/panel", "")
	if err != nil || cfg.Addr != "h:3306" || cfg.DBName != "panel" {
		t.Errorf("dsn: %+v %v", cfg, err)
	}
}

func TestImportNeedsSource(t *testing.T) {
	t.Setenv("GAMEPANEL_PTERO_DSN", "")
	if err := importPterodactyl(nil); err == nil {
		t.Error("ran without a source")
	}
	if err := importPterodactyl([]string{"--dsn", "u:p@tcp(h:1)/x", "--unlimited-memory", "10"}); err == nil {
		t.Error("tiny --unlimited-memory accepted")
	}
}
