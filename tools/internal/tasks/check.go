// check is the task CI and a working copy both run: formatting, go vet and the
// tests, from the same entry point, so the two can never drift apart.
package tasks

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	p := bindPipeline(fs)
	if e := parse(fs, args); e != nil {
		return e
	}
	out, logPath, e := openLog("check", p.race)
	if e != nil {
		return e
	}
	defer out.Close()
	// The vendored security patch is checked here too: it is a plain checksum of
	// the tree, so CI catches a re-vendored MyGo that silently dropped it.
	if e := verifyVendorPatch(); e != nil {
		fmt.Fprintf(out, "\n补丁校验失败：%s\n", logPath)
		return e
	}
	fmt.Fprintf(out, "== patches/vendor-integrity.json 校验通过\n")
	if e := checkFormatting(out); e != nil {
		fmt.Fprintf(out, "\n格式检查失败：%s\n", logPath)
		return e
	}
	return p.run(out, logPath)
}

// checkFormatting reports the files gofmt would rewrite, without rewriting them:
// a check that edits files cannot be run on someone else's tree.
func checkFormatting(out *tee) error {
	files, e := moduleGoFiles()
	if e != nil {
		return e
	}
	gofmt, e := gofmtPath()
	if e != nil {
		return e
	}
	var listed bytes.Buffer
	cmd := exec.Command(gofmt, append([]string{"-l"}, files...)...)
	cmd.Stdout, cmd.Stderr = &listed, out
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("gofmt 无法运行: %w", e)
	}
	unformatted := strings.TrimSpace(listed.String())
	if unformatted != "" {
		fmt.Fprintf(out, "== gofmt -l\n%s\n", unformatted)
		return errors.New("gofmt 未通过；对上面列出的文件运行 gofmt -w 后重试")
	}
	fmt.Fprintf(out, "== gofmt -l %d 个文件全部已格式化\n", len(files))
	return nil
}

// moduleGoFiles lists the module's Go files, leaving out the vendored tree and
// anything hidden (build output, editor state).
func moduleGoFiles() ([]string, error) {
	var files []string
	e := filepath.WalkDir(".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if path != "." && (name == "vendor" || strings.HasPrefix(name, ".") || name == "build") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, e
}

// gofmtPath finds gofmt beside the go command, so the check does not depend on
// PATH holding the same toolchain.
func gofmtPath() (string, error) {
	out, e := exec.Command("go", "env", "GOROOT").Output()
	if e != nil {
		return "", fmt.Errorf("无法定位 GOROOT: %w", e)
	}
	name := "gofmt"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(strings.TrimSpace(string(out)), "bin", name)
	if _, e := os.Stat(path); e != nil {
		return "", fmt.Errorf("找不到 gofmt: %w", e)
	}
	return path, nil
}
