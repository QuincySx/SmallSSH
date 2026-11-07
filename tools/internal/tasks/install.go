package tasks

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sshclient/tools/internal/project"
)

// runInstall publishes a verified Mac bundle without launching it. Both the
// replaced bundle and any failed staging copy remain recoverable in Trash.
func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("用法: go run ./tools install")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("install 当前只支持将本机架构的 macOS 包安装到 /Applications")
	}
	if _, err := project.Chdir(); err != nil {
		return err
	}
	out, logPath, err := openLog("install", false)
	if err != nil {
		return err
	}
	defer out.Close()
	source := filepath.Join("build", "darwin-"+runtime.GOARCH, "SmallSSH.app")
	const destination = "/Applications/SmallSSH.app"
	bundleID := func(bundle string) (string, error) {
		b, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", filepath.Join(bundle, "Contents", "Info.plist")).Output()
		return strings.TrimSpace(string(b)), err
	}
	id, err := bundleID(source)
	if err != nil || id == "" {
		return fmt.Errorf("未找到有效的本机应用包；请先运行 tools build -platform darwin/%s", runtime.GOARCH)
	}
	if err = checkBundleDaemon(source); err != nil {
		return err
	}
	old, err := os.Lstat(destination)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		oldID, idErr := bundleID(destination)
		if !old.IsDir() || idErr != nil || oldID != id {
			return errors.New("目标不是同一应用的普通目录，拒绝替换")
		}
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trash := filepath.Join(homeDir, ".Trash")
	if err = os.MkdirAll(trash, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("/Applications", ".SmallSSH-install-*.app")
	if err != nil {
		return err
	}
	defer func() {
		if stage != "" {
			failed := filepath.Join(trash, filepath.Base(stage))
			if err := os.Rename(stage, failed); err != nil {
				fmt.Fprintf(out, "未完成的副本保留在 %s：%v\n", stage, err)
			}
		}
	}()
	if err = step(out, "/usr/bin/ditto", source, stage); err != nil {
		return err
	}
	if err = step(out, "/usr/bin/codesign", "--verify", "--deep", "--strict", "--verbose=2", stage); err != nil {
		return err
	}
	if err = checkBundleDaemon(stage); err != nil {
		return err
	}
	backup := ""
	if old != nil {
		backup = filepath.Join(trash, "SmallSSH-before-"+time.Now().Format("20060102-150405.000000000")+".app")
		if err = os.Rename(destination, backup); err != nil {
			return err
		}
	}
	if err = os.Rename(stage, destination); err != nil {
		if backup != "" {
			if rollback := os.Rename(backup, destination); rollback != nil {
				return fmt.Errorf("安装失败：%v；还原失败：%v；旧版在 %s", err, rollback, backup)
			}
		}
		return err
	}
	stage = ""
	fmt.Fprintf(out, "已安装：%s\n", destination)
	if backup != "" {
		fmt.Fprintf(out, "旧版保留：%s\n", backup)
	}
	fmt.Fprintln(out, "未启动应用；日志："+logPath)
	return nil
}

func checkBundleDaemon(bundle string) error {
	daemon := filepath.Join(bundle, "Contents", "Resources", "smallsshd")
	info, err := os.Lstat(daemon)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("应用包缺少后台程序 %s，拒绝安装不完整的包", daemon)
		}
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("后台程序 %s 是符号链接，拒绝安装", daemon)
	case !info.Mode().IsRegular():
		return fmt.Errorf("后台程序 %s 不是普通文件，拒绝安装", daemon)
	case info.Mode().Perm()&0o111 == 0:
		return fmt.Errorf("后台程序 %s 没有执行权限，拒绝安装", daemon)
	}
	return nil
}
