package fileattrs

import "os"

func openAttributeHandle(name string) (*os.File, error) { return os.Open(name) }
