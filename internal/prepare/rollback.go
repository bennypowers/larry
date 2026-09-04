package prepare

import (
	"os"
	"path/filepath"
)

// Backup creates a .bak backup of a file.
func Backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".bak", data, 0644)
}

// Restore restores a .bak backup.
func Restore(path string) error {
	data, err := os.ReadFile(path + ".bak")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// BackupDir creates .bak backups for all files in a directory.
func BackupDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if filepath.Ext(path) != ".bak" {
			return Backup(path)
		}
		return nil
	})
}
