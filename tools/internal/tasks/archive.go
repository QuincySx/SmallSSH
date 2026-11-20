package tasks

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"sshclient/tools/internal/project"
)

// Archive existing bundles without following symlinks or losing execute bits.
// CI and local publishing preparation use the same task and output directory.
func runArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	platforms := fs.String("platform", "all", "comma-separated packaged targets, or all")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("用法: go run ./tools archive [-platform 目标]")
	}
	if _, err := project.Chdir(); err != nil {
		return err
	}
	targets, err := parseTargets(*platforms)
	if err != nil {
		return err
	}
	out, logPath, err := openLog("archive", false)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, target := range targets {
		name := target.goos + "-" + target.goarch
		destination := filepath.Join("build", "dist", "smallssh-"+name+".tar.gz")
		if err := archiveDirectory(filepath.Join("build", name), destination); err != nil {
			return fmt.Errorf("%s 归档失败：%w", target, err)
		}
		fmt.Fprintln(out, "已归档："+destination)
	}
	fmt.Fprintln(out, "全部归档通过："+logPath)
	return nil
}

func archiveDirectory(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("打包产物必须是普通目录")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	compressed := gzip.NewWriter(file)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(archive, input)
		closeErr := input.Close()
		return errors.Join(err, closeErr)
	}); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}
