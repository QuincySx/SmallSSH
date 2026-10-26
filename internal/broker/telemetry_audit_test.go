package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestAutomaticTelemetryDoesNotFloodAudit(t *testing.T) {
	var counters, inventories, docker, unexpected atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			n := counters.Add(1)
			return fmt.Sprintf("@@CPU\ncpu %d 0 100 %d 0 0 0 0\n@@MEM\nMemTotal: 524288 kB\nMemAvailable: 300000 kB\n", 100+n*10, 800+n*90), "", 0
		case monitor.InventoryCommand:
			inventories.Add(1)
			return "@@OS\nTest Linux\n@@SERVICE_MANAGER\nsystemd\n@@DOCKER_STATUS\nmissing\n", "", 0
		case monitor.DockerStatsCommand:
			docker.Add(1)
			// A failed background sub-probe must not create audit noise either.
			return "", "fixture Docker unavailable", 1
		default:
			unexpected.Add(1)
			return "", "unexpected fixture command", 1
		}
	})
	dir, err := os.MkdirTemp("", "ssh-telemetry-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	owner, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	owner.Put(fixture.Profile())
	if err := vault.Save(broker.VaultPath(dir), owner, true); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	server, err := broker.OpenSession(dir, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	cli := client.Client{Dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, ticket, err := cli.Request(ctx, approval.Operation{
		Host: "fixture", Kind: "shell", ServerStatus: true, Client: "test terminal",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	approve(t, cli, request)
	stream, err := cli.Start(ctx, request.ID, ticket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	auditPath := filepath.Join(dir, "audit.jsonl")
	beforeSamples, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	var inventorySeen, dockerFailureSeen bool
	for samples := 0; samples < 3; {
		message, err := stream.Receive()
		if err != nil {
			t.Fatal("automatic sampling stopped", err)
		}
		if message.Action != "stats" || message.Stats == nil {
			t.Fatal("expected a resource sample", message.Action)
		}
		if message.Stats.MemoryTotal == 0 {
			t.Fatal("resource sample did not contain fixture data")
		}
		inventorySeen = inventorySeen || message.Stats.Inventory != nil
		dockerFailureSeen = dockerFailureSeen || message.Stats.DockerStats != nil && message.Stats.DockerStats.Status == "unavailable"
		samples++
	}
	if counters.Load() < 3 || inventories.Load() == 0 || docker.Load() == 0 || !inventorySeen || !dockerFailureSeen {
		t.Fatal("telemetry sub-probes were not exercised", counters.Load(), inventories.Load(), docker.Load(), inventorySeen, dockerFailureSeen)
	}
	afterSamples, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeSamples, afterSamples) {
		t.Fatal("automatic samples or sub-probe failures grew the operation audit")
	}
	if fixture.Auths.Load() != 1 {
		t.Fatal("telemetry opened separate SSH connections", fixture.Auths.Load())
	}

	// An identical command supplied as an ordinary request remains audited.
	manual, manualTicket, err := cli.Request(ctx, approval.Operation{
		Host: "fixture", Kind: "exec", Command: monitor.InventoryCommand, Client: "内部系统采样",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	approve(t, cli, manual)
	manualStream, err := cli.Start(ctx, manual.ID, manualTicket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	code, _, copyErr := manualStream.Copy(io.Discard, io.Discard)
	manualStream.Close()
	if copyErr != nil || code != 0 {
		t.Fatal("ordinary fixture request failed", code, copyErr)
	}
	denied, _, err := cli.Request(ctx, approval.Operation{
		Host: "fixture", Kind: "exec", Command: monitor.Command, Client: "内部系统采样",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Decide(ctx, denied.ID, false, false, pass); err != nil {
		t.Fatal(err)
	}
	// A public field cannot turn arbitrary exec into silent internal telemetry.
	if _, _, err := cli.Request(ctx, approval.Operation{
		Host: "fixture", Kind: "exec", Command: monitor.InventoryCommand,
		ServerStatus: true, Client: "内部系统采样",
	}, time.Minute); err == nil {
		t.Fatal("caller-controlled status flag allowed a disguised exec request")
	}
	stream.Close()
	// Wait for all execution defers, including ended, before checking the log.
	server.Close()
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]map[string]int)
	totalEvents := 0
	invalidOperationID := ""
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var event struct {
			Event     string `json:"event"`
			RequestID string `json:"requestId"`
			Reason    string `json:"reason"`
		}
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal("invalid audit event", err)
		}
		totalEvents++
		if counts[event.RequestID] == nil {
			counts[event.RequestID] = make(map[string]int)
		}
		counts[event.RequestID][event.Event]++
		if event.Reason == "invalid_operation" {
			if invalidOperationID != "" || event.Event != "denied" || event.RequestID == "" {
				t.Fatal("invalid operation refusal was duplicated or lost its request identity")
			}
			invalidOperationID = event.RequestID
		}
	}
	for _, id := range []string{request.ID, manual.ID} {
		for _, event := range []string{"approved", "started", "ended"} {
			if counts[id][event] != 1 {
				t.Errorf("operation %s should have one %s event; got %d", id, event, counts[id][event])
			}
		}
	}
	if counts[denied.ID]["denied"] != 1 || counts[denied.ID]["started"] != 0 {
		t.Error("denied ordinary request lost its audit or was executed", counts[denied.ID])
	}
	for _, id := range []string{request.ID, manual.ID, denied.ID} {
		if counts[id]["requested"] != 1 {
			t.Error("ordinary request should have exactly one requested event", id, counts[id])
		}
	}
	if invalidOperationID == "" || counts[invalidOperationID]["denied"] != 1 || counts[invalidOperationID]["started"] != 0 || counts[invalidOperationID]["requested"] != 0 {
		t.Error("disguised telemetry request must be refused before authority is created", counts[invalidOperationID])
	}
	if counts[""]["unlocked"] != 1 || counts[""]["locked"] != 1 {
		t.Error("session boundary events were lost", counts[""])
	}
	// Boundary events, three ordinary request events, their decisions/runs, and
	// the rejected caller-supplied ServerStatus disguise are expected. This
	// also catches first-round sub-probes that may finish before beforeSamples.
	if totalEvents != 13 {
		t.Error("unexpected per-probe or duplicate audit events", totalEvents)
	}
	if unexpected.Load() != 0 {
		t.Fatal("test reached a non-fixture command", unexpected.Load())
	}
}
