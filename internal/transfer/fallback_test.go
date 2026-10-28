package transfer

import (
	"errors"
	"testing"

	"sshclient/internal/client"
	"sshclient/internal/vault"
)

// Only an unprivileged upload that was refused for being a resumed write may be
// retried from the beginning; the owner and downloads keep their semantics.
func TestFreshUploadFallbackOnlyForRefusedResumedUploads(t *testing.T) {
	refused := errors.Join(client.ErrAutomationPolicy, errors.New("路径保护模式不允许无法限定文件影响范围的操作"))
	other := errors.New("audit unavailable")
	owner := &vault.Session{}
	for _, tc := range []struct {
		name      string
		kind      string
		owner     *vault.Session
		direction string
		err       error
		want      bool
	}{
		{"refused resumed upload", "put-resume", nil, "upload", refused, true},
		{"refused fresh upload", "put", nil, "upload", refused, false},
		{"refused resumed download", "get-resume", nil, "download", refused, false},
		{"owner keeps its semantics", "put-resume", owner, "upload", refused, false},
		{"unrelated failure", "put-resume", nil, "upload", other, false},
		{"no failure", "put-resume", nil, "upload", nil, false},
	} {
		if got := freshUploadFallback(tc.kind, tc.owner, tc.direction, tc.err); got != tc.want {
			t.Fatalf("%s: freshUploadFallback = %v, want %v", tc.name, got, tc.want)
		}
	}
}
