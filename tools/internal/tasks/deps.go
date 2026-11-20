package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"sshclient/tools/internal/project"
)

func runDeps(args []string) error {
	fs := flag.NewFlagSet("deps", flag.ContinueOnError)
	update := fs.Bool("update", false, "explicitly regenerate vendor using versions already pinned in go.mod")
	if err := parse(fs, args); err != nil {
		return err
	}
	if !*update {
		return errors.New("依赖更新需显式使用 deps -update；版本先在 go.mod 固定")
	}
	if _, err := project.Chdir(); err != nil {
		return err
	}
	if err := verifyVendorPatch(); err != nil {
		return err
	}
	out, logPath, err := openLog("deps", false)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, arguments := range [][]string{{"mod", "tidy"}, {"mod", "vendor"}} {
		cmd := exec.Command("go", arguments...)
		cmd.Env = proxyProbeEnv(os.Environ(), map[string]string{"GOFLAGS": "", "GOWORK": "off", "GIT_TERMINAL_PROMPT": "0"})
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("依赖更新失败，日志 %s: %w", logPath, err)
		}
	}
	for _, patch := range []string{"patches/mygo-macos-password-accessibility.patch", "patches/sing-vmess-vision-pointer.patch"} {
		if err := step(out, "git", "apply", "--check", patch); err != nil {
			return fmt.Errorf("上游代码有变化，补丁 %s 需要重新审核：%w", patch, err)
		}
		if err := step(out, "git", "apply", patch); err != nil {
			return err
		}
	}
	// An existing hash must still match after reapplying the reviewed patch.
	// Never bless a changed native security implementation by refreshing hashes.
	if err := verifyVendorPatch(); err != nil {
		return err
	}
	var hashes map[string]string
	content, err := os.ReadFile("patches/vendor-integrity.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, &hashes); err != nil {
		return err
	}
	for file := range hashes {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		hashes[file] = hex.EncodeToString(sum[:])
	}
	encoded, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		return err
	}
	if err := generate("patches/vendor-integrity.json", append(encoded, '\n'), false); err != nil {
		return err
	}
	if err := stageNotices(false); err != nil {
		return err
	}
	fmt.Fprintln(out, "依赖与许可证已更新；原生密码框补丁及哈希重新验证通过："+logPath)
	return nil
}
