package desktop

import "golang.org/x/sys/unix"

func renameLocalExclusive(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
