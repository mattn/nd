package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

type Config struct {
	Server   string  `json:"server"`
	User     string  `json:"user"`
	Password string  `json:"password"`
	MPV      string  `json:"mpv,omitempty"`
	Theme    string  `json:"theme,omitempty"`
	Colors   *Colors `json:"colors,omitempty"`
	Cover    *bool   `json:"cover,omitempty"` // show cover art with sixel; default true
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "nd", "config.json")
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

func main() {
	server := flag.String("server", "", "Navidrome server URL (e.g. http://localhost:4533)")
	user := flag.String("user", "", "Username")
	password := flag.String("password", "", "Password")
	mpv := flag.String("mpv", "", "Path to mpv binary")
	theme := flag.String("theme", "", "Color theme for this session (not saved)")
	listThemes := flag.Bool("list-themes", false, "List available color themes")
	flag.Parse()

	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = &Config{}
	}

	if *listThemes {
		printThemes(os.Stdout, cfg.Theme)
		return
	}

	if *server != "" {
		cfg.Server = *server
	}
	if *user != "" {
		cfg.User = *user
	}
	if *password != "" {
		cfg.Password = *password
	}
	if *mpv != "" {
		cfg.MPV = *mpv
	}

	if cfg.Server == "" || cfg.User == "" || cfg.Password == "" {
		fmt.Fprintln(os.Stderr, "Usage: nd -server URL -user USER -password PASS")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "Or create %s:\n", configPath())
		fmt.Fprintln(os.Stderr, `  {"server": "http://localhost:4533", "user": "admin", "password": "pass", "mpv": "/usr/bin/mpv"}`)
		os.Exit(1)
	}

	// -theme is temporary: use the theme as-is, without config overrides
	var err error
	if *theme != "" {
		err = applyTheme(*theme, nil)
	} else {
		err = applyTheme(cfg.Theme, cfg.Colors)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid theme: %v\n", err)
		os.Exit(1)
	}

	// Save config for next time
	_ = saveConfig(cfg)

	client := NewSubsonicClient(cfg.Server, cfg.User, cfg.Password)

	// Test connection
	if err := client.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to server: %v\n", err)
		os.Exit(1)
	}

	mpvPath := cfg.MPV
	if mpvPath == "" {
		mpvPath = "mpv"
	}
	player := NewPlayer(mpvPath)
	defer player.Cleanup()

	var cover coverConfig
	if cfg.Cover == nil || *cfg.Cover {
		cover = detectCover()
	}

	m := newModel(client, player, cover)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
