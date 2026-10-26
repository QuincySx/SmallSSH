// build packages the desktop and matching CLI together without publishing.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if e := build(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func build() error {
	targets := flag.String("platform", "darwin/arm64,linux/amd64,windows/amd64", "comma-separated desktop targets")
	flag.Parse()
	var hashes map[string]string
	b, e := os.ReadFile("patches/vendor-integrity.json")
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &hashes); e != nil {
		return e
	}
	for file, expected := range hashes {
		b, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != expected {
			return errors.New("vendored 安全补丁不匹配；请先核对 patches/ 中的补丁")
		}
	}
	var notices strings.Builder
	notices.WriteString("Third-party license texts from vendored dependencies.\n\n")
	if e = filepath.WalkDir("vendor", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		n := strings.ToUpper(d.Name())
		if strings.HasPrefix(n, "LICENSE") || strings.HasPrefix(n, "LICENCE") || n == "COPYING" || n == "NOTICE" {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			fmt.Fprintf(&notices, "\n===== %s =====\n\n%s\n", p, b)
		}
		return nil
	}); e != nil {
		return e
	}
	if e = os.WriteFile("resources/THIRD_PARTY_NOTICES.txt", []byte(notices.String()), 0644); e != nil {
		return e
	}
	// Compile and sign the native verifier before embedding its checksum in Go.
	var native map[string]string
	b, _ = os.ReadFile("internal/systemauth/native-hashes.json")
	_ = json.Unmarshal(b, &native)
	if native == nil {
		native = map[string]string{}
	}
	for _, target := range strings.Split(*targets, ",") {
		if !strings.HasPrefix(target, "darwin/") {
			continue
		}
		arch := strings.TrimPrefix(target, "darwin/")
		swiftArch := arch
		if arch == "amd64" {
			swiftArch = "x86_64"
		}
		for _, artifact := range []struct {
			source, name, hashKey string
			library               bool
		}{
			{"Biometric.swift", "biometric-helper", target, false},
			{"EmbeddedBiometric.swift", "libsmallssh-auth.dylib", target + "/embedded", true},
		} {
			out := filepath.Join("resources", "darwin-"+arch, artifact.name)
			if e = os.MkdirAll(filepath.Dir(out), 0755); e != nil {
				return e
			}
			args := []string{"-O", "-target", swiftArch + "-apple-macosx13.0"}
			if artifact.library {
				args = append(args, "-emit-library", "-Xlinker", "-install_name", "-Xlinker", "@rpath/"+artifact.name)
			}
			args = append(args, filepath.Join("native", "macos", artifact.source), "-o", out)
			cmd := exec.Command("/usr/bin/swiftc", args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if e = cmd.Run(); e != nil {
				return e
			}
			cmd = exec.Command("/usr/bin/codesign", "--force", "--sign", "-", "--timestamp=none", out)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if e = cmd.Run(); e != nil {
				return e
			}
			b, e = os.ReadFile(out)
			if e != nil {
				return e
			}
			h := sha256.Sum256(b)
			native[artifact.hashKey] = hex.EncodeToString(h[:])
		}
	}
	b, _ = json.MarshalIndent(native, "", "  ")
	if e = os.WriteFile("internal/systemauth/native-hashes.json", b, 0644); e != nil {
		return e
	}
	for _, target := range strings.Split(*targets, ",") {
		goos, arch, ok := strings.Cut(target, "/")
		if !ok || (goos != "darwin" && goos != "linux" && goos != "windows") || (arch != "arm64" && arch != "amd64") {
			return fmt.Errorf("invalid target %q", target)
		}
		name := "smallssh"
		legacyName := "ssh-client"
		if goos == "windows" {
			name += ".exe"
			legacyName += ".exe"
		}
		resourceDir := filepath.Join("resources", goos+"-"+arch)
		out := filepath.Join(resourceDir, "cli", name)
		if e = os.MkdirAll(filepath.Dir(out), 0755); e != nil {
			return e
		}
		cmd := exec.Command("go", "build", "-trimpath", "-o", out, "./cmd/ssh-client")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e = cmd.Run(); e != nil {
			return e
		}
		// Keep the legacy command byte-for-byte identical for existing integrations.
		if e = copyExecutable(out, filepath.Join(resourceDir, legacyName)); e != nil {
			return e
		}
	}
	cmd := exec.Command("go", "run", "github.com/egoist/mygo/cmd/mygo@v0.5.5", "build", "-platform", *targets, "-skip-dmg")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// Replace the alias through a temporary file instead of truncating the old executable.
func copyExecutable(src, dst string) error {
	b, e := os.ReadFile(src)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0755); e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), dst)
}
