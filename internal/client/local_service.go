package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

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

// Ensure starts only the fixed, verified headless executable when its socket is
// absent. An existing endpoint that fails identity checks is never replaced.
func (s *LocalService) Ensure(ctx context.Context) error {
	if _, err := s.client.Status(ctx); err == nil {
		return nil
	} else {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if conn, dialErr := localipc.Dial(probe, filepath.Join(s.client.Dir, "service")); dialErr == nil {
			conn.Close()
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The lock serializes simultaneous frontend starts without trusting socket existence.
	if err := localipc.Prepare(s.client.Dir); err != nil {
		return err
	}
	release, err := localipc.Acquire(filepath.Join(s.client.Dir, "service-start"))
	if err != nil {
		return s.waitReady(ctx)
	}
	defer release()
	if _, err := s.client.Status(ctx); err == nil {
		return nil
	}
	command := exec.Command(s.client.Executable, "--data-dir", s.client.Dir)
	configureServiceProcess(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动固定本地服务: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := s.client.Status(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return fmt.Errorf("本地服务未就绪即退出: %v", err)
		case <-timer.C:
			return errors.New("本地服务启动超时")
		case <-tick.C:
		}
	}
}
func (s *LocalService) waitReady(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := s.client.Status(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("等待本地服务启动超时")
		case <-tick.C:
		}
	}
}
