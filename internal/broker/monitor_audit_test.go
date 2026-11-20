package broker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestMonitorActualProbeIsAuditedAndDiskFailureStopsExecution(t *testing.T) {
	var calls atomic.Int32
	f := testserver.NewWithExec(t, func(string) (string, string, int) { calls.Add(1); return "fixture", "", 0 })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := sshconn.Dial(ctx, f.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	owner, err := vault.New([]byte("synthetic monitor audit only"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	log, err := openAudit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer log.close()
	s := &Server{audit: log, Vault: owner, Approvals: approval.New(nil)}
	execute := s.monitorExecutor(connection, f.Profile().Fingerprint, monitorAuditSource{Host: "synthetic"})
	if _, err := execute(ctx, monitor.Command, time.Second); err != nil {
		t.Fatal(err)
	}
	page, err := log.history(auditlog.Query{Category: "system"})
	if err != nil || len(page.Events) != 2 || page.Events[0].Event != "system_ended" || page.Events[0].Outcome != "success" || page.Events[0].Command != monitor.Command || page.Events[0].Executable == "" {
		t.Fatal("system execution was not reviewable", err)
	}
	log.close()
	if _, err := execute(ctx, monitor.InventoryCommand, time.Second); err == nil || calls.Load() != 1 || !s.auditFault.Load() {
		t.Fatal("system probe bypassed audit failure", calls.Load(), err)
	}
}
