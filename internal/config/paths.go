package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "tsuzuki"

// Paths holds every on-disk location tsuzuki uses.
type Paths struct {
	ConfigDir string // config.toml, auth token
	DataDir   string // database
	CacheDir  string // logs, HTTP/browser caches
}

func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.toml") }
func (p Paths) Database() string   { return filepath.Join(p.DataDir, "tsuzuki.db") }
func (p Paths) LogFile() string    { return filepath.Join(p.CacheDir, "tsuzuki.log") }

// ResolvePaths returns platform-appropriate directories. Each can be
// overridden with TSUZUKI_CONFIG_DIR, TSUZUKI_DATA_DIR or TSUZUKI_CACHE_DIR.
func ResolvePaths() (Paths, error) {
	configBase, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	dataBase, err := userDataDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		ConfigDir: envOr("TSUZUKI_CONFIG_DIR", filepath.Join(configBase, appName)),
		DataDir:   envOr("TSUZUKI_DATA_DIR", filepath.Join(dataBase, appName)),
		CacheDir:  envOr("TSUZUKI_CACHE_DIR", filepath.Join(cacheBase, appName)),
	}, nil
}

// userDataDir is the missing counterpart to os.UserConfigDir.
func userDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("LocalAppData"); dir != "" {
			return dir, nil
		}
		return os.UserConfigDir()
	case "darwin", "ios":
		return os.UserConfigDir() // ~/Library/Application Support
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
			return dir, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
