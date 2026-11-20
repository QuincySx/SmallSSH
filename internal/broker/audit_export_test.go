package broker_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func TestAuditExportsAllFilteredRecordsWithHashesAndSafeCSV(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	for i := 0; i < 34; i++ {
		r, _, err := c.Request(ctx, approval.Operation{Host: "fixture", Kind: "exec", Command: "=SYNTHETIC_ONLY()", Client: "audit-export-fixture"}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Decide(ctx, r.ID, false, false, pass); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	q := auditlog.Query{Search: "audit-export-fixture"}
	n, err := c.ExportHistory(ctx, &output, q, "JSONL", owner)
	if err != nil || n != 68 {
		t.Fatal("export stopped at visible page", n, err)
	}
	scan := bufio.NewScanner(&output)
	scan.Buffer(make([]byte, 4096), 1<<20)
	if !scan.Scan() || !bytes.Contains(scan.Bytes(), []byte("smallssh-audit/v1")) {
		t.Fatal("missing export scope")
	}
	rows := 0
	for scan.Scan() {
		var e auditlog.Event
		if json.Unmarshal(scan.Bytes(), &e) != nil || e.RequestID == "" || e.Executable == "" || e.ProcessInstance == "" {
			t.Fatal("missing provenance in exported fact")
		}
		h := sha256.Sum256(scan.Bytes())
		if len(hex.EncodeToString(h[:])) != 64 || e.RecordHash != "" {
			t.Fatal("export mutated original record")
		}
		rows++
	}
	if err := scan.Err(); err != nil || rows != n {
		t.Fatal(rows, err)
	}
	output.Reset()
	n, err = c.ExportHistory(ctx, &output, q, "CSV", owner)
	if err != nil || n != 68 {
		t.Fatal(n, err)
	}
	reader := csv.NewReader(&output)
	if _, err := reader.Read(); err != nil {
		t.Fatal(err)
	}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(row[12], "'=SYNTHETIC_ONLY") {
			t.Fatal("CSV cell could execute a spreadsheet formula", row[12])
		}
	}
	if _, err := c.ExportHistory(ctx, io.Discard, q, "JSONL", nil); err == nil {
		t.Fatal("locked caller exported history")
	}
}
