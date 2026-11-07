//go:build !darwin

package localipc

import (
	"os"
	"strings"
)

func executableCodeID(path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", ErrIdentityUnavailable
	}
	before, err := os.Stat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", ErrIdentityUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrIdentityUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameImage(before, opened) {
		return "", ErrIdentityUnavailable
	}
	code, _, err := executableDigest(file)
	return code, err
}
