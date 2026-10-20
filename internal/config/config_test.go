package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	d := Default()
	if cfg.Server.Port != d.Server.Port || cfg.Media.Root != d.Media.Root ||
		cfg.Media.PageSize != d.Media.PageSize || cfg.Auth.Enabled {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ListenAddr() != ":8080" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr())
	}
}

func TestValidConfig(t *testing.T) {
	dir := t.TempDir()
	// A media root that exists.
	root := filepath.Join(dir, "photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := writeFile(t, dir, "config.yaml", `
server:
  host: "127.0.0.1"
  port: 9090

media:
  root: "`+root+`"
  page_size: 25

auth:
  enabled: true
  username: "alice"
  password: "s3cret"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9090 {
		t.Errorf("server: %+v", cfg.Server)
	}
	if cfg.Media.Root != root || cfg.Media.PageSize != 25 {
		t.Errorf("media: %+v", cfg.Media)
	}
	if !cfg.Auth.Enabled || cfg.Auth.Username != "alice" || cfg.Auth.Password != "s3cret" {
		t.Errorf("auth: %+v", cfg.Auth)
	}
	if cfg.ListenAddr() != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr())
	}
}

func TestPartialConfigKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := writeFile(t, dir, "config.yaml", "media:\n  root: \""+root+"\"\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 || cfg.Media.PageSize != 50 {
		t.Errorf("defaults not preserved: %+v", cfg)
	}
}

func TestEmptyConfigFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "empty.yaml", "")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("empty config should keep defaults, got %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port = %d", cfg.Server.Port)
	}
}

func TestInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "bad.yaml", "server:\n  port: [unclosed\n")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestUnknownKey(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := writeFile(t, dir, "c.yaml", "media:\n  root: \""+root+"\"\n  pag_size: 10\n")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "pag_size") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func TestMissingMediaRoot(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "c.yaml", "media:\n  root: \"/definitely/not/here\"\n")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing media root")
	}
}

func TestMediaRootIsFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "notadir", "x")
	q := writeFile(t, dir, "c.yaml", "media:\n  root: \""+p+"\"\n")
	if _, err := Load(q); err == nil {
		t.Fatal("expected error when media root is a file")
	}
}

func TestInvalidPort(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"0", "-1", "70000"} {
		p := writeFile(t, dir, "c.yaml", "server:\n  port: "+port+"\nmedia:\n  root: \""+root+"\"\n")
		if _, err := Load(p); err == nil {
			t.Errorf("expected error for port %s", port)
		}
	}
}

func TestInvalidPageSize(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, size := range []string{"0", "-5", "501"} {
		p := writeFile(t, dir, "c.yaml", "media:\n  root: \""+root+"\"\n  page_size: "+size+"\n")
		if _, err := Load(p); err == nil {
			t.Errorf("expected error for page_size %s", size)
		}
	}
}

func TestAuthMissingCredentials(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"auth:\n  enabled: true\n",
		"auth:\n  enabled: true\n  username: \"admin\"\n",
		"auth:\n  enabled: true\n  password: \"pw\"\n",
	}
	for i, c := range cases {
		p := writeFile(t, dir, "c.yaml", "media:\n  root: \""+root+"\"\n"+c)
		if _, err := Load(p); err == nil {
			t.Errorf("case %d: expected error for auth without credentials", i)
		}
	}
}

func TestMissingConfigFile(t *testing.T) {
	if _, err := Load("/definitely/not/a/config.yaml"); err == nil {
		t.Fatal("expected error for unreadable config file")
	}
}
