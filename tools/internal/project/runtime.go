package project

import "strings"

const RuntimeTags = "with_utls,with_quic,with_grpc"
const RuntimeLinkFlags = "-checklinkname=0"
const ReleaseLinkFlags = "-s -w " + RuntimeLinkFlags

func Tags(extra string) string {
	if extra == "" {
		return RuntimeTags
	}
	return RuntimeTags + "," + extra
}

// MyGo preserves GOFLAGS tags but appends its own linker flags, so its explicit
// MYGO_LDFLAGS extension is needed as well. All task paths use the same features.
func RuntimeEnv(base []string) []string {
	result := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if !strings.HasPrefix(entry, "GOFLAGS=") && !strings.HasPrefix(entry, "MYGO_LDFLAGS=") {
			result = append(result, entry)
		}
	}
	return append(result, "GOFLAGS=-tags="+RuntimeTags, "MYGO_LDFLAGS="+RuntimeLinkFlags)
}
