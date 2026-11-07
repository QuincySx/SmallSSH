package client

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"sshclient/internal/localipc"
)

type LocalService struct {
	client ServiceClient
}

func NewLocalService(dir string) (*LocalService, error) {
	if dir == "" {
		dir = localipc.DefaultDir()
	} else {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		dir = absolute
	}

	entry, err := os.Executable()
	if err != nil {
		return nil, err
	}
	entry, err = filepath.EvalSymlinks(entry)
	if err != nil {
		return nil, err
	}
	daemon, err := daemonExecutablePath(entry, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	if err := validateDaemonExecutable(daemon, runtime.GOOS); err != nil {
		return nil, err
	}
	if _, err := localipc.ExecutableCodeID(daemon); err != nil {
		return nil, err
	}

	return &LocalService{client: ServiceClient{Dir: dir, Executable: daemon}}, nil
}

func (s *LocalService) Client() ServiceClient {
	return s.client
}

func daemonExecutablePath(entry, goos string) (string, error) {
	if !filepath.IsAbs(entry) {
		return "", errors.New("程序入口必须是绝对路径")
	}
	entry = filepath.Clean(entry)
	name := "smallsshd"
	if goos == "windows" {
		name += ".exe"
	}

	parent := filepath.Dir(entry)
	if goos == "darwin" && filepath.Base(parent) == "MacOS" && filepath.Base(filepath.Dir(parent)) == "Contents" {
		return filepath.Join(filepath.Dir(parent), "Resources", name), nil
	}
	if filepath.Base(parent) == "cli" {
		return filepath.Join(filepath.Dir(parent), name), nil
	}
	return filepath.Join(parent, name), nil
}

func validateDaemonExecutable(path, goos string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("固定本地服务程序必须是普通文件")
	}
	if goos != "windows" && info.Mode().Perm()&0111 == 0 {
		return errors.New("固定本地服务程序不可执行")
	}
	return nil
}
