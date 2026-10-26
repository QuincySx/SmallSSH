package broker

import (
	"encoding/json"
	"sshclient/internal/auditlog"
)

// AuditHistoryProof binds an owner challenge to the exact page query, including
// its cursor and limit. Reading history never creates an audit event of its own.
func AuditHistoryProof(challenge string, q auditlog.Query, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	return prove(append([]byte("ssh-client/audit-history/v1\x00"+challenge+"\x00"), b...))
}
