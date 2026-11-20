package vault

import (
	"os"
	"path/filepath"
)

func Path(dir string) string { return filepath.Join(dir, "vault.json") }
func Exists(dir string) bool { _, err := os.Stat(Path(dir)); return err == nil }
