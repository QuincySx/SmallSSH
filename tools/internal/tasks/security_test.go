package tasks

import (
	"strings"
	"testing"
)

func TestSecurityJSONDistinguishesReachableFindingsAndRejectsMissingResults(t *testing.T) {
	stream := `{"config":{"scanner_name":"govulncheck"}}
{"finding":{"osv":"GO-synthetic-module","trace":[{"module":"unused.invalid"}]}}
{"finding":{"osv":"GO-synthetic-called","trace":[{"function":"Decode"},{"function":"main"}]}}
{"finding":{"osv":"GO-synthetic-called","trace":[{"function":"Other"}]}}`
	findings, err := securityFindings(strings.NewReader(stream))
	if err != nil || len(findings) != 1 || !findings["GO-synthetic-called"] {
		t.Fatal("JSON mode lost a reachable finding or treated module metadata as reachable", err)
	}
	for _, input := range []string{"", `{"progress":{}}`, `{"config":{}} {"finding":`} {
		if _, err := securityFindings(strings.NewReader(input)); err == nil {
			t.Fatal("missing or incomplete scanner output counted as zero vulnerabilities")
		}
	}
}
