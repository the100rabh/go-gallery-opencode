// Package config loads and validates the YAML configuration file.
//
// Design notes:
//   - The config file is optional. If no path is given (or the file is
//     empty), sensible defaults are used.
//   - A config file overrides defaults field by field; unknown keys are a
//     hard error so typos in paths like "pag_size" fail loudly at startup.
//   - Validation is strict and fails clearly: bad YAML, missing/invalid
//     media root, invalid port, invalid page size, and auth enabled
//     without credentials all abort startup.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultHost      = "" // all interfaces
	defaultPort      = 8080
	defaultMediaRoot = "/media"
	defaultPageSize  = 50
	maxPageSize      = 500
)

// Server describes the HTTP listener.
type Server struct {
	Host string `yaml:"host"` // "" means all interfaces
	Port int    `yaml:"port"`
}

// Media describes the served media directory.
type Media struct {
	Root     string `yaml:"root"`
	PageSize int    `yaml:"page_size"`
}

// Auth describes HTTP Basic authentication.
type Auth struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Config is the whole configuration file.
type Config struct {
	Server Server `yaml:"server"`
	Media  Media  `yaml:"media"`
	Auth   Auth   `yaml:"auth"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Server: Server{Host: defaultHost, Port: defaultPort},
		Media:  Media{Root: defaultMediaRoot, PageSize: defaultPageSize},
	}
}

// Load reads the optional YAML file at path, applies it over the defaults
// and validates the result. An empty path means "use defaults only".
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("config: cannot read %q: %w", path, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true) // reject unknown/misspelled keys
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return cfg, fmt.Errorf("config: invalid YAML in %q: %w", path, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate checks the configuration and returns the first problem found.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("config: server.port must be between 1 and 65535 (got %d)", c.Server.Port)
	}
	if c.Media.PageSize < 1 || c.Media.PageSize > maxPageSize {
		return fmt.Errorf("config: media.page_size must be between 1 and %d (got %d)", maxPageSize, c.Media.PageSize)
	}
	if c.Media.Root == "" {
		return errors.New("config: media.root must not be empty")
	}
	info, err := os.Stat(c.Media.Root)
	if err != nil {
		return fmt.Errorf("config: media.root %q is not accessible (must exist and be a directory): %v", c.Media.Root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config: media.root %q is not a directory", c.Media.Root)
	}
	if c.Auth.Enabled && (c.Auth.Username == "" || c.Auth.Password == "") {
		return errors.New("config: auth.enabled requires auth.username and auth.password")
	}
	return nil
}

// ListenAddr returns the address the HTTP server should bind.
func (c *Config) ListenAddr() string {
	host := c.Server.Host
	if host == ":" {
		host = ""
	}
	return fmt.Sprintf("%s:%d", host, c.Server.Port)
}
