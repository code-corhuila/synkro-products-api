package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pemValue = "-----BEGIN PUBLIC KEY-----\nplaceholder\n-----END PUBLIC KEY-----\n"

func setEnv(t *testing.T, db, key, keyFile string) {
	t.Helper()
	t.Setenv("DATABASE_URL", db)
	t.Setenv("JWT_PUBLIC_KEY", key)
	t.Setenv("JWT_PUBLIC_KEY_FILE", keyFile)
	t.Setenv("HTTP_PORT", "")
}

func TestLoad_ReadsThePublicKeyFromTheEnvironment(t *testing.T) {
	setEnv(t, "postgres://x", pemValue, "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWTPublicKey != pemValue || cfg.HTTPPort != "8080" || cfg.DatabaseURL != "postgres://x" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoad_ReadsThePublicKeyFromAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwt.pub")
	if err := os.WriteFile(path, []byte(pemValue), 0o600); err != nil {
		t.Fatal(err)
	}
	setEnv(t, "postgres://x", "", path)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWTPublicKey != pemValue {
		t.Errorf("key not read from the file: %q", cfg.JWTPublicKey)
	}
}

func TestLoad_RequiresExactlyOneKeySource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwt.pub")
	_ = os.WriteFile(path, []byte(pemValue), 0o600)

	for name, c := range map[string]struct{ key, file string }{
		"neither":        {"", ""},
		"both":           {pemValue, path},
		"missing file":   {"", filepath.Join(t.TempDir(), "absent.pub")},
		"empty file":     {"", func() string { p := filepath.Join(t.TempDir(), "empty"); _ = os.WriteFile(p, nil, 0o600); return p }()},
		"blank variable": {"   \n", ""},
	} {
		setEnv(t, "postgres://x", c.key, c.file)
		if _, err := Load(); err == nil {
			t.Errorf("%s: want an error, got none", name)
		}
	}
}

func TestLoad_StillRequiresTheDatabaseURL(t *testing.T) {
	setEnv(t, "", pemValue, "")
	if _, err := Load(); err == nil {
		t.Error("want an error without DATABASE_URL")
	}
}

func TestLoad_ErrorsNeverEchoKeyMaterial(t *testing.T) {
	setEnv(t, "postgres://x", pemValue, "/some/file")
	_, err := Load()
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); len(got) > 200 || strings.Contains(got, "placeholder") {
		t.Errorf("error echoes key material: %q", got)
	}
}
