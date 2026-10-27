// Command tools is the repository's front door: `go run ./tools` lists the
// development tasks, and `go run ./tools <task>` runs one. Each task is also its
// own command, so `go run ./tools/build -platform darwin/arm64` does the same
// thing. Go has no npm scripts, so this is the convention that replaces them.
package main

import "sshclient/tools/internal/tasks"

func main() { tasks.Main("") }
