// Package config resolves the shared local configuration for all commands.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	DataDir string
	Timeout time.Duration
	APIAddr string
	NoAPI   bool
}

func Default() (Config, error) {
	dir := os.Getenv("WASYNC_DATA_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve configuration directory: %w", err)
		}
		dir = filepath.Join(base, "wasync")
	}
	addr := os.Getenv("WASYNC_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	return Config{DataDir: dir, Timeout: 3 * time.Minute, APIAddr: addr}, nil
}

func (c Config) Validate() (Config, error) {
	if c.DataDir == "" {
		return c, fmt.Errorf("data directory cannot be empty")
	}
	if c.Timeout <= 0 {
		return c, fmt.Errorf("timeout must be positive")
	}
	dir, err := filepath.Abs(c.DataDir)
	if err != nil {
		return c, fmt.Errorf("resolve data directory: %w", err)
	}
	if dir == filepath.VolumeName(dir)+string(filepath.Separator) {
		return c, fmt.Errorf("use a dedicated data directory, not the filesystem root")
	}
	c.DataDir = dir
	return c, nil
}
