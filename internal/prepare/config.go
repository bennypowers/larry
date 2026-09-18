package prepare

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config represents per-package settings from .larry.yaml.
type Config struct {
	// TypeOverride forces the package type, overriding auto-detection.
	TypeOverride string
	// VendorDirs maps package names to source subdirectories containing vendored deps.
	// Format: { "pkgname": ["src/web", "lib/deps"] }
	VendorDirs map[string][]string
	// CleanupOrphanedAssets removes orphaned GitHub release assets on prepare failure.
	CleanupOrphanedAssets bool
}

// LoadConfig reads .larry.yaml from the ebuild's directory and returns
// the parsed configuration. Returns a zero Config if the file does not exist.
func LoadConfig(ebuildPath string) (*Config, error) {
	dir := filepath.Dir(ebuildPath)
	configPath := filepath.Join(dir, ".larry.yaml")

	v := viper.New()
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read .larry.yaml: %w", err)
		}
		return &Config{}, nil
	}

	cfg := &Config{
		VendorDirs: make(map[string][]string),
	}

	// type_override: forces package type detection
	if v.Get("type_override") != nil {
		cfg.TypeOverride = v.GetString("type_override")
	}

	// cleanup_orphaned_assets: removes orphaned GitHub assets on failure
	cfg.CleanupOrphanedAssets = v.GetBool("cleanup_orphaned_assets")

	// vendor_dirs: maps package names to vendored source dirs
	if v.Get("vendor_dirs") != nil {
		if vd, ok := v.Get("vendor_dirs").(map[string]interface{}); ok {
			for pkg, val := range vd {
				switch v := val.(type) {
				case string:
					cfg.VendorDirs[pkg] = []string{v}
				case []interface{}:
					dirs := make([]string, 0, len(v))
					for _, d := range v {
						if s, ok := d.(string); ok {
							dirs = append(dirs, s)
						}
					}
					cfg.VendorDirs[pkg] = dirs
				}
			}
		}
	}

	return cfg, nil
}
