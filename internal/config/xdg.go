package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "agora"

type pathEnv struct {
	lookupEnv func(string) (string, bool)
	userHome  func() (string, error)
}

func defaultPathEnv() pathEnv {
	return pathEnv{
		lookupEnv: os.LookupEnv,
		userHome:  os.UserHomeDir,
	}
}

// ConfigDir returns Agora's platform-appropriate global configuration directory.
func ConfigDir() (string, error) {
	return configDirFor(runtime.GOOS, defaultPathEnv())
}

// DataDir returns Agora's platform-appropriate global data directory.
func DataDir() (string, error) {
	return dataDirFor(runtime.GOOS, defaultPathEnv())
}

// GlobalConfigPath returns the default global config.yaml path.
func GlobalConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// TranscriptStoreDir returns the default managed transcript store directory.
func TranscriptStoreDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "transcripts"), nil
}

func configDirFor(goos string, env pathEnv) (string, error) {
	switch goos {
	case "darwin":
		home, err := env.userHome()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", appDirName), nil
	case "windows":
		base, ok := env.lookupEnv("LOCALAPPDATA")
		if !ok || base == "" {
			home, err := env.userHome()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, appDirName), nil
	default:
		base, ok := env.lookupEnv("XDG_CONFIG_HOME")
		if !ok || base == "" {
			home, err := env.userHome()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, appDirName), nil
	}
}

func dataDirFor(goos string, env pathEnv) (string, error) {
	switch goos {
	case "darwin":
		home, err := env.userHome()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", appDirName), nil
	case "windows":
		base, ok := env.lookupEnv("LOCALAPPDATA")
		if !ok || base == "" {
			home, err := env.userHome()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, appDirName), nil
	default:
		base, ok := env.lookupEnv("XDG_DATA_HOME")
		if !ok || base == "" {
			home, err := env.userHome()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, appDirName), nil
	}
}
