package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config stores the resolved environment needed to run opencode.
type Config struct {
	OpenCodeBinary string `json:"opencodeBinary"`
	NodePath       string `json:"nodePath"`
	WorkingDir     string `json:"workingDir"`
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".opengate", "config.json")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func writeConfig(path string, cfg Config) error {
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// autoConfig resolves the real opencode binary and its companion node dir,
// writes them to ~/.opengate/config.json and prints the result.
func autoConfig() error {
	bin := OpenCodeBinary
	if bin == "" {
		bin = "opencode"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("could not find opencode binary %q in PATH: %w", bin, err)
	}
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = real
	}

	cwd, _ := os.Getwd()
	cfg := Config{
		OpenCodeBinary: resolved,
		NodePath:       inferNodePath(resolved),
		WorkingDir:     cwd,
	}

	path := defaultConfigPath()
	if err := writeConfig(path, cfg); err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}

	fmt.Printf("Config written to %s\n\n", path)
	fmt.Printf("%s\n", cfg)
	return nil
}

// inferNodePath derives the companion node bin directory from the real
// opencode binary path. For an fnm install the opencode binary lives at:
//
//	<fnm>/<version>/installation/lib/node_modules/opencode-ai/bin/opencode.exe
//
// and the node binary is at <fnm>/<version>/installation/bin. Falls back to
// the node found in PATH.
func inferNodePath(real string) string {
	marker := "/installation/lib/node_modules/"
	idx := strings.Index(real, marker)
	if idx >= 0 {
		installationDir := real[:idx+len("/installation")]
		candidate := filepath.Join(installationDir, "bin")
		if _, err := os.Stat(filepath.Join(candidate, "node")); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("node"); err == nil {
		return filepath.Dir(p)
	}
	return ""
}
