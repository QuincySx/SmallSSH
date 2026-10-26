package workbench

import _ "embed"

// AppIconPNG is also the artwork used to generate the native package icons.
// The embedding declaration stays outside the packaged runtime resources.
//
//go:embed resources/icon.png
var AppIconPNG []byte
