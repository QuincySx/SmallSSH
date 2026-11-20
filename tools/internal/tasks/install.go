package tasks

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"sshclient/tools/internal/project"
)

// runInstall publishes a verified Mac bundle without launching it or keeping
// old bundles. The old bundle exists only until replacement succeeds.
func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	candidate := fs.String("source", filepath.Join("build", "darwin-"+runtime.GOARCH, "SmallSSH.app"), "已验证的本机应用包路径")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("用法: go run ./tools install [-source 应用包路径]")
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
	source := filepath.Clean(*candidate)
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
	stage, err := os.MkdirTemp("/Applications", ".SmallSSH-install-*.app")
	if err != nil {
		return err
	}
	defer func() {
		if stage != "" {
			if err := os.RemoveAll(stage); err != nil {
				fmt.Fprintf(out, "清理安装临时目录失败 %s：%v\n", stage, err)
			}
		}
	}()
	if err = step(out, "/usr/bin/ditto", source, stage); err != nil {
		return err
	}
	if err = reuseInstalledExecutables(stage, destination); err != nil {
		return err
	}
	if err = step(out, "/usr/bin/codesign", "--verify", "--deep", "--strict", "--verbose=2", stage); err != nil {
		return err
	}
	if err = checkBundleDaemon(stage); err != nil {
		return err
	}
	if err = replaceInstalledBundle(stage, destination); err != nil {
		return err
	}
	stage = ""
	fmt.Fprintf(out, "已安装：%s\n", destination)
	fmt.Fprintln(out, "未启动应用；日志："+logPath)
	return nil
}

// macOS validates a running process against its executable vnode. Replacing an
// unchanged file with a fresh copy can invalidate that identity after the old
// bundle is removed. Keep the inode for byte-identical programs, then validate
// the complete candidate's signature before publishing it.
func reuseInstalledExecutables(stage, destination string) error {
	for _, rel := range []string{
		filepath.Join("Contents", "MacOS", "SmallSSH"),
		filepath.Join("Contents", "Resources", "smallsshd"),
		filepath.Join("Contents", "Resources", "cli", "smallssh"),
	} {
		oldPath, newPath := filepath.Join(destination, rel), filepath.Join(stage, rel)
		oldInfo, err := os.Lstat(oldPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		newInfo, err := os.Lstat(newPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !oldInfo.Mode().IsRegular() || !newInfo.Mode().IsRegular() ||
			oldInfo.Mode() != newInfo.Mode() || oldInfo.Size() != newInfo.Size() ||
			os.SameFile(oldInfo, newInfo) {
			continue
		}
		oldBytes, err := os.ReadFile(oldPath)
		if err != nil {
			return err
		}
		newBytes, err := os.ReadFile(newPath)
		if err != nil {
			return err
		}
		if sha256.Sum256(oldBytes) != sha256.Sum256(newBytes) {
			continue
		}
		link := newPath + ".unchanged"
		if err := os.Link(oldPath, link); err != nil {
			return err
		}
		if err := os.Rename(link, newPath); err != nil {
			_ = os.Remove(link)
			return err
		}
	}
	return nil
}

// Keep rollback possible during the rename, then remove the previous bundle.
// A failed rollback leaves its recovery path in the error, outside stage so
// runInstall's failed-candidate cleanup cannot remove the last working app.
func replaceInstalledBundle(stage, destination string) error {
	previous := stage + ".previous"
	if _, err := os.Lstat(previous); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("安装临时路径已被占用：%s", previous)
	}
	hadOld := false
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, previous); err != nil {
			return err
		}
		hadOld = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		if hadOld {
			if rollback := os.Rename(previous, destination); rollback != nil {
				return fmt.Errorf("安装失败：%v；还原失败：%v；原应用暂存于 %s", err, rollback, previous)
			}
		}
		return err
	}
	if hadOld {
		if err := os.RemoveAll(previous); err != nil {
			return fmt.Errorf("新版已安装，清理原应用失败 %s：%w", previous, err)
		}
	}
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
