package transfer

import (
	"errors"
	"testing"

	"sshclient/internal/client"
	"sshclient/internal/ownercap"
	"sshclient/internal/vault"
)

type fallbackProofOnly struct{}

func (fallbackProofOnly) Proof([]byte) ([]byte, error) { return nil, errors.New("synthetic proof") }

// Only an unprivileged upload that was refused for being a resumed write may be
// retried from the beginning; the owner and downloads keep their semantics.
func TestFreshUploadFallbackOnlyForRefusedResumedUploads(t *testing.T) {
	refused := errors.Join(client.ErrAutomationPolicy, errors.New("路径保护模式不允许无法限定文件影响范围的操作"))
	other := errors.New("audit unavailable")
	owner := &vault.Session{}
	var nilSession *vault.Session
	var nilSigner *ownercap.Signer
	for _, tc := range []struct {
		name      string
		kind      string
		owner     client.OwnerProof
		direction string
		err       error
		want      bool
	}{
		{"refused resumed upload", "put-resume", nil, "upload", refused, true},
		{"refused fresh upload", "put", nil, "upload", refused, false},
		{"refused resumed download", "get-resume", nil, "download", refused, false},
		{"owner keeps its semantics", "put-resume", owner, "upload", refused, false},
		{"proof-only owner keeps its semantics", "put-resume", fallbackProofOnly{}, "upload", refused, false},
		{"typed nil session remains public", "put-resume", nilSession, "upload", refused, true},
		{"typed nil signer remains public", "put-resume", nilSigner, "upload", refused, true},
		{"unrelated failure", "put-resume", nil, "upload", other, false},
		{"no failure", "put-resume", nil, "upload", nil, false},
	} {
		if got := freshUploadFallback(tc.kind, tc.owner, tc.direction, tc.err); got != tc.want {
			t.Fatalf("%s: freshUploadFallback = %v, want %v", tc.name, got, tc.want)
		}
	}
}
