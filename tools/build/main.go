// build is the same task as `go run ./tools build`; both live in tools/internal/tasks.
package main

import "sshclient/tools/internal/tasks"

func main() { tasks.Main("build") }
