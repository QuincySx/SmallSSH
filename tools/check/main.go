// check is the same task as `go run ./tools check`; both live in tools/internal/tasks.
package main

import "sshclient/tools/internal/tasks"

func main() { tasks.Main("check") }
