// Package project locates the module root and reads the pinned tool versions the
// tools need, so each tool can run from any directory and no version is written
// down twice.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Root returns the module root, walking up from the working directory.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("找不到 go.mod；请在项目目录内运行")
		}
		dir = parent
	}
}

// Chdir changes to the module root, which every tool works from.
func Chdir() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return root, os.Chdir(root)
}

// Require returns the version go.mod requires for module, read through
// `go mod edit -json` so the file is not parsed by hand.
func Require(module string) (string, error) {
	out, err := exec.Command("go", "mod", "edit", "-json").Output()
	if err != nil {
		return "", fmt.Errorf("读取 go.mod 失败: %w", err)
	}
	var mod struct {
		Require []struct {
			Path, Version string
		}
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", fmt.Errorf("解析 go.mod 失败: %w", err)
	}
	for _, r := range mod.Require {
		if r.Path == module {
			return r.Version, nil
		}
	}
	return "", fmt.Errorf("go.mod 未声明 %s", module)
}

// MyGo returns the pinned MyGo version, which the packaging CLI must match: the
// library in vendor/ and the CLI that packages the app are one module.
func MyGo() (string, error) { return Require("github.com/egoist/mygo") }
