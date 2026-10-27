// build stages the native components and the per-platform CLI, then packages the
// desktop app.
//
// mygo copies resources/<goos> and resources/<goos>-<goarch> into the app's
// resource directory, so those directories are where the generated binaries have
// to be. They hold nothing but build output, git ignores them, and every run
// removes the target's directory first: no file from an earlier build can ship.
// The packaged apps, the disk images and the logs stay in build/, which is also
// ignored, so a build leaves the working tree as it found it.
//
// Two generated files are committed on purpose and rewritten here only when their
// content changes: internal/systemauth/native-hashes.json pins the native
// components the app rejects if they do not match, and
// resources/THIRD_PARTY_NOTICES.txt must ship with the app. -check verifies both
// without writing, for review and CI.
package tasks

import (
	"bytes"
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

	"sshclient/tools/internal/project"
)

const (
	resourcesDir = "resources"
	hashesPath   = "internal/systemauth/native-hashes.json"
	noticesPath  = "resources/THIRD_PARTY_NOTICES.txt"
)

// target is one platform the app is packaged for.
// runBuild stages and packages the app.
func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	platforms := fs.String("platform", "darwin/arm64,linux/amd64,windows/amd64", `comma-separated desktop targets, or "all"`)
	check := fs.Bool("check", false, "只校验已提交的生成文件是否为最新，不写入任何内容")
	if e := parse(fs, args); e != nil {
		return e
	}
	return build(*platforms, *check)
}

type target struct{ goos, goarch string }

func (t target) String() string { return t.goos + "/" + t.goarch }

// resources is the directory whose contents ship only with this platform's app.
func (t target) resources() string {
	if t.goos == "darwin" {
		return filepath.Join(resourcesDir, "darwin-"+t.goarch)
	}
	return filepath.Join(resourcesDir, t.goos+"-"+t.goarch)
}

func build(platforms string, check bool) error {
	if _, e := project.Chdir(); e != nil {
		return e
	}
	targets, e := parseTargets(platforms)
	if e != nil {
		return e
	}
	if e = verifyVendorPatch(); e != nil {
		return e
	}
	if !check {
		for _, t := range targets {
			if e = os.RemoveAll(t.resources()); e != nil {
				return e
			}
		}
	}
	if e = stageNotices(check); e != nil {
		return e
	}
	hashes, e := stageNatives(targets, check)
	if e != nil {
		return e
	}
	if e = stageHashes(hashes, check); e != nil {
		return e
	}
	if check {
		fmt.Println("生成文件均为最新：" + noticesPath + "、" + hashesPath)
		return nil
	}
	for _, t := range targets {
		if e = stageCLI(t); e != nil {
			return e
		}
	}
	if e = packageApp(platforms); e != nil {
		return e
	}
	fmt.Printf("已暂存 %d 个目标（%s）；打包输出与日志在 build/\n", len(targets), platforms)
	return nil
}

// everyTarget is the release list: every desktop the project ships. `-platform
// all` expands to it, so the workflow and a workstation build the same set.
const everyTarget = "darwin/arm64,darwin/amd64,linux/amd64,linux/arm64,windows/amd64"

func parseTargets(list string) ([]target, error) {
	if strings.TrimSpace(list) == "all" {
		list = everyTarget
	}
	var targets []target
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		goos, goarch, ok := strings.Cut(item, "/")
		if !ok || (goos != "darwin" && goos != "linux" && goos != "windows") || (goarch != "arm64" && goarch != "amd64") {
			return nil, fmt.Errorf("invalid target %q", item)
		}
		targets = append(targets, target{goos, goarch})
	}
	if len(targets) == 0 {
		return nil, errors.New("未指定打包目标")
	}
	return targets, nil
}

// verifyVendorPatch guards the vendored MyGo security fix: a re-vendored tree
// must not silently drop it.
func verifyVendorPatch() error {
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
	return nil
}

// stageNotices collects the license texts of everything vendored, which the app
// has to ship.
func stageNotices(check bool) error {
	var notices strings.Builder
	notices.WriteString("Third-party license texts from vendored dependencies.\n\n")
	e := filepath.WalkDir("vendor", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		n := strings.ToUpper(d.Name())
		if strings.HasPrefix(n, "LICENSE") || strings.HasPrefix(n, "LICENCE") || n == "COPYING" || n == "NOTICE" {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(&notices, "\n===== %s =====\n\n%s\n", p, b)
		}
		return nil
	})
	if e != nil {
		return e
	}
	return generate(noticesPath, []byte(notices.String()), check)
}

// stageNatives compiles and signs the macOS components for every darwin target
// and returns the checksums the app verifies them against. Check mode compiles
// into a temporary directory, which is discarded.
func stageNatives(targets []target, check bool) (map[string]string, error) {
	previous, e := os.ReadFile(hashesPath)
	if e != nil {
		return nil, e
	}
	var hashes map[string]string
	if e = json.Unmarshal(previous, &hashes); e != nil {
		return nil, e
	}
	if hashes == nil {
		hashes = map[string]string{}
	}
	staging, e := os.MkdirTemp("", "smallssh-natives-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(staging)
	for _, t := range targets {
		if t.goos != "darwin" {
			continue
		}
		swiftArch := t.goarch
		if t.goarch == "amd64" {
			swiftArch = "x86_64"
		}
		dir := t.resources()
		if check {
			dir = filepath.Join(staging, "darwin-"+t.goarch)
		}
		for _, artifact := range []struct {
			source, name, hashKey string
			library               bool
		}{
			{"Biometric.swift", "biometric-helper", t.String(), false},
			{"EmbeddedBiometric.swift", "libsmallssh-auth.dylib", t.String() + "/embedded", true},
		} {
			out := filepath.Join(dir, artifact.name)
			if e = os.MkdirAll(filepath.Dir(out), 0o755); e != nil {
				return nil, e
			}
			args := []string{"-O", "-target", swiftArch + "-apple-macosx13.0"}
			if artifact.library {
				args = append(args, "-emit-library", "-Xlinker", "-install_name", "-Xlinker", "@rpath/"+artifact.name)
			}
			args = append(args, filepath.Join("native", "macos", artifact.source), "-o", out)
			if e = run("/usr/bin/swiftc", args...); e != nil {
				return nil, e
			}
			if e = run("/usr/bin/codesign", "--force", "--sign", "-", "--timestamp=none", out); e != nil {
				return nil, e
			}
			b, err := os.ReadFile(out)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(b)
			hashes[artifact.hashKey] = hex.EncodeToString(sum[:])
		}
	}
	return hashes, nil
}

// stageHashes writes the checksums of the components this build produced. A run
// that packages only one architecture keeps the checksums of the others, which
// its own builds regenerate.
func stageHashes(hashes map[string]string, check bool) error {
	b, e := json.MarshalIndent(hashes, "", "  ")
	if e != nil {
		return e
	}
	return generate(hashesPath, append(b, '\n'), check)
}

// stageCLI builds the CLI for one platform next to the app, and the legacy name
// existing integrations run.
func stageCLI(t target) error {
	name, legacy := "smallssh", "ssh-client"
	if t.goos == "windows" {
		name, legacy = name+".exe", legacy+".exe"
	}
	out := filepath.Join(t.resources(), "cli", name)
	if e := os.MkdirAll(filepath.Dir(out), 0o755); e != nil {
		return e
	}
	cmd := exec.Command("go", "build", "-mod=vendor", "-trimpath", "-o", out, "./cmd/ssh-client")
	cmd.Env = append(os.Environ(), "GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if e := cmd.Run(); e != nil {
		return e
	}
	return copyExecutable(out, filepath.Join(t.resources(), legacy))
}

// packageApp runs the pinned MyGo CLI, which matches the vendored library read
// from go.mod, so the app is packaged by the same version it links against.
func packageApp(platforms string) error {
	version, e := project.MyGo()
	if e != nil {
		return e
	}
	return run("go", "run", "github.com/egoist/mygo/cmd/mygo@"+version, "build", "-platform", platforms, "-skip-dmg")
}

// generate writes b to path unless it is already there, and in check mode
// reports a committed file that a build would change.
func generate(path string, b []byte, check bool) error {
	old, e := os.ReadFile(path)
	switch {
	case e == nil && bytes.Equal(old, b):
		return nil
	case e != nil && !os.IsNotExist(e):
		return e
	}
	if check {
		return fmt.Errorf("%s 不是最新；运行 go run ./tools/build 更新并提交", path)
	}
	if e = os.MkdirAll(filepath.Dir(path), 0o755); e != nil {
		return e
	}
	if e = os.WriteFile(path, b, 0o644); e != nil {
		return e
	}
	fmt.Println("已更新 " + path)
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
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
	if e = f.Chmod(0o755); e != nil {
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
