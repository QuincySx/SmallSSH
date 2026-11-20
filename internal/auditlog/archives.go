package auditlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Segments are retained until the owner removes the whole workbench. Rotation
// bounds individual files, never the amount of history available for audit.
func (s *Store) paths() ([]string, error) {
	entries, err := os.ReadDir(filepath.Dir(s.path))
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(s.path) + ".archive-"
	var archives []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		n := strings.TrimPrefix(e.Name(), prefix)
		if len(n) != 20 {
			return nil, errors.New("审计归档文件名无效")
		}
		if _, err := strconv.ParseUint(n, 10, 64); err != nil {
			return nil, errors.New("审计归档文件名无效")
		}
		archives = append(archives, filepath.Join(filepath.Dir(s.path), e.Name()))
	}
	sort.Strings(archives)
	return append(archives, s.path+".1", s.path), nil
}

func (s *Store) retainArchive() error {
	f, err := openPrivate(s.path+".1", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f.Close()
	paths, err := s.paths()
	if err != nil {
		return err
	}
	var next uint64 = 1
	if len(paths) > 2 {
		last := paths[len(paths)-3]
		n, err := strconv.ParseUint(strings.TrimPrefix(last, s.path+".archive-"), 10, 64)
		if err != nil || n == ^uint64(0) {
			return errors.New("审计归档编号无效")
		}
		next = n + 1
	}
	destination := fmt.Sprintf("%s.archive-%020d", s.path, next)
	// Link is exclusive: an unexpected file can never be replaced. The broker
	// owns the directory lock. A crash between link and unlink fails the private
	// file/link checks on reopen instead of silently losing or duplicating data.
	if err := os.Link(s.path+".1", destination); err != nil {
		return err
	}
	if err := os.Remove(s.path + ".1"); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}
