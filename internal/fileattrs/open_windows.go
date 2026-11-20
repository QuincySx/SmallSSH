package fileattrs

import "os"

func openAttributeHandle(name string) (localAttributeHandle, error) { return os.Open(name) }
