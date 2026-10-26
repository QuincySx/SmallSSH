package monitor

import "strings"

// /proc/mounts uses octal escapes for whitespace and backslashes in paths.
func decodeMount(path string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(path)
}
