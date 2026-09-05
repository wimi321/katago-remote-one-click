package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ConfigFileName     = "config.json"
	TokenFileName      = "state/access-token"
	ConnectionFileName = "state/connection.json"
	PIDFileName        = "state/service.json"
	LogFileName        = "logs/service.log"
)

type Config struct {
	Listen            string        `json:"listen"`
	KataGoPath        string        `json:"katagoPath"`
	ModelPath         string        `json:"modelPath"`
	AnalysisConfig    string        `json:"analysisConfig"`
	CloudflaredPath   string        `json:"cloudflaredPath"`
	MaxMessageBytes   int64         `json:"maxMessageBytes"`
	MaxVisits         int64         `json:"maxVisits"`
	StartupTimeoutSec int           `json:"startupTimeoutSeconds"`
	TunnelTimeoutSec  int           `json:"tunnelTimeoutSeconds"`
	ExtraEnvironment  []string      `json:"extraEnvironment,omitempty"`
	StartupTimeout    time.Duration `json:"-"`
	TunnelTimeout     time.Duration `json:"-"`
}

func Default() Config {
	return Config{
		Listen:            "127.0.0.1:8765",
		MaxMessageBytes:   4 << 20,
		MaxVisits:         1_000_000,
		StartupTimeoutSec: 45,
		TunnelTimeoutSec:  45,
	}
}

func DefaultHome() (string, error) {
	if value := strings.TrimSpace(os.Getenv("KATAGO_REMOTE_HOME")); value != "" {
		return filepath.Abs(value)
	}
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		return filepath.Abs(filepath.Join(dataHome, "katago-remote-one-click"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "katago-remote-one-click"), nil
}

func Load(home string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(home, ConfigFileName))
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.StartupTimeout = time.Duration(cfg.StartupTimeoutSec) * time.Second
	cfg.TunnelTimeout = time.Duration(cfg.TunnelTimeoutSec) * time.Second
	return cfg, nil
}

func (cfg Config) Validate() error {
	if !strings.HasPrefix(cfg.Listen, "127.0.0.1:") && !strings.HasPrefix(cfg.Listen, "localhost:") {
		return errors.New("listen address must remain on localhost")
	}
	for name, value := range map[string]string{
		"katagoPath":      cfg.KataGoPath,
		"modelPath":       cfg.ModelPath,
		"analysisConfig":  cfg.AnalysisConfig,
		"cloudflaredPath": cfg.CloudflaredPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
	}
	if cfg.MaxMessageBytes < 1024 || cfg.MaxMessageBytes > 64<<20 {
		return errors.New("maxMessageBytes must be between 1 KiB and 64 MiB")
	}
	if cfg.MaxVisits < 1 || cfg.MaxVisits > 100_000_000 {
		return errors.New("maxVisits must be between 1 and 100000000")
	}
	if cfg.StartupTimeoutSec < 5 || cfg.StartupTimeoutSec > 300 {
		return errors.New("startupTimeoutSeconds must be between 5 and 300")
	}
	if cfg.TunnelTimeoutSec < 5 || cfg.TunnelTimeoutSec > 300 {
		return errors.New("tunnelTimeoutSeconds must be between 5 and 300")
	}
	for _, item := range cfg.ExtraEnvironment {
		if !strings.Contains(item, "=") || strings.HasPrefix(item, "=") {
			return errors.New("extraEnvironment entries must use NAME=value")
		}
	}
	return nil
}

func Save(home string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := EnsurePrivateDirs(home); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(home, ConfigFileName), data, 0o600)
}

func EnsurePrivateDirs(home string) error {
	for _, path := range []string{home, filepath.Join(home, "state"), filepath.Join(home, "logs")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("secure %s: %w", path, err)
		}
	}
	return nil
}

func EnsureToken(home string, rotate bool) (string, error) {
	path := filepath.Join(home, TokenFileName)
	if !rotate {
		if data, err := os.ReadFile(path); err == nil {
			token := strings.TrimSpace(string(data))
			if len(token) >= 32 {
				return token, nil
			}
		}
	}
	if err := EnsurePrivateDirs(home); err != nil {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate access token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	if err := atomicWrite(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func ReadToken(home string) (string, error) {
	data, err := os.ReadFile(filepath.Join(home, TokenFileName))
	if err != nil {
		return "", fmt.Errorf("read access token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 {
		return "", errors.New("access token is missing or invalid; run init again")
	}
	return token, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secure %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
