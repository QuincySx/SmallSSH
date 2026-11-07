//go:build windows

package localipc

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func executableEndpointUnavailable(err error) bool {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "open" {
		return false
	}
	return errors.Is(pathErr.Err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(pathErr.Err, windows.ERROR_PATH_NOT_FOUND)
}
