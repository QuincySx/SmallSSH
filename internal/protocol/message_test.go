package protocol_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

func TestMessageJSONFieldContract(t *testing.T) {
	want := []struct {
		name string
		typ  string
		tag  string
	}{
		{"RouteMeasurements", "[]sshconn.RouteMeasurement", `json:"routeMeasurements,omitempty"`},
		{"ExpectedFingerprint", "string", `json:"expectedFingerprint,omitempty"`},
		{"ListenAddress", "string", `json:"listenAddress,omitempty"`},
		{"Network", "*connectcfg.Network", `json:"network,omitempty"`},
		{"KeepProxyCredential", "bool", `json:"keepProxyCredential,omitempty"`},
		{"Channel", "string", `json:"channel,omitempty"`},
		{"Challenge", "string", `json:"challenge,omitempty"`},
		{"After", "string", `json:"after,omitempty"`},
		{"Next", "string", `json:"next,omitempty"`},
		{"Limit", "int", `json:"limit,omitempty"`},
		{"Redacted", "bool", `json:"redacted,omitempty"`},
		{"AuditQuery", "*auditlog.Query", `json:"auditQuery,omitempty"`},
		{"AuditPage", "*auditlog.Page", `json:"auditPage,omitempty"`},
		{"Renewal", "*approval.RenewalChallenge", `json:"renewal,omitempty"`},
		{"ErrorCode", "string", `json:"errorCode,omitempty"`},
		{"ComposePlan", "*serverops.ComposePlan", `json:"composePlan,omitempty"`},
		{"ComposeStatus", "[]serverops.ComposeStatus", `json:"composeStatus,omitempty"`},
		{"Phase", "string", `json:"phase,omitempty"`},
		{"FileInfo", "*fileattrs.Info", `json:"fileInfo,omitempty"`},
		{"Delegation", "*approval.DelegationChallenge", `json:"delegation,omitempty"`},
		{"HostIdentity", "*sshconn.HostIdentity", `json:"hostIdentity,omitempty"`},
		{"Identities", "[]sshconn.KeyInfo", `json:"identities,omitempty"`},
		{"ProfileChange", "*vault.ProfileChange", `json:"profileChange,omitempty"`},
		{"Port", "int", `json:"port,omitempty"`},
		{"MonitorError", "string", `json:"monitorError,omitempty"`},
		{"MonitorActive", "bool", `json:"monitorActive,omitempty"`},
		{"Transfer", "*sshconn.TransferMeta", `json:"transfer,omitempty"`},
		{"Stats", "*monitor.Stats", `json:"stats,omitempty"`},
		{"Action", "string", `json:"action,omitempty"`},
		{"Operation", "approval.Operation", `json:"operation,omitempty"`},
		{"TTL", "int", `json:"ttl,omitempty"`},
		{"Idle", "int", `json:"idle,omitempty"`},
		{"ID", "string", `json:"id,omitempty"`},
		{"Ticket", "string", `json:"ticket,omitempty"`},
		{"Allow", "bool", `json:"allow,omitempty"`},
		{"Proof", "[]uint8", `json:"proof,omitempty"`},
		{"Request", "*approval.Request", `json:"request,omitempty"`},
		{"Requests", "[]approval.Request", `json:"requests,omitempty"`},
		{"Hosts", "[]string", `json:"hosts,omitempty"`},
		{"Files", "[]sshconn.File", `json:"files,omitempty"`},
		{"Data", "[]uint8", `json:"data,omitempty"`},
		{"Cols", "int", `json:"cols,omitempty"`},
		{"Rows", "int", `json:"rows,omitempty"`},
		{"ExitCode", "int", `json:"exitCode,omitempty"`},
		{"Error", "string", `json:"error,omitempty"`},
		{"Profile", "*vault.Profile", `json:"profile,omitempty"`},
		{"Connections", "[]vault.Summary", `json:"connections,omitempty"`},
		{"Details", "*vault.Summary", `json:"details,omitempty"`},
		{"Target", "string", `json:"target,omitempty"`},
		{"Workspace", "*protocol.WorkspaceQuery", `json:"workspace,omitempty"`},
		{"WorkspaceResult", "*protocol.WorkspaceResult", `json:"workspaceResult,omitempty"`},
		{"WorkspaceChange", "*protocol.WorkspaceChange", `json:"workspaceChange,omitempty"`},
	}

	typ := reflect.TypeOf(protocol.Message{})
	if typ.NumField() != len(want) {
		t.Fatalf("Message has %d fields, want %d", typ.NumField(), len(want))
	}
	for index, expected := range want {
		field := typ.Field(index)
		if field.Name != expected.name || field.Type.String() != expected.typ || string(field.Tag) != expected.tag {
			t.Errorf("field %d = %s %s %s, want %s %s %s", index, field.Name, field.Type, field.Tag, expected.name, expected.typ, expected.tag)
		}
	}
}

func TestMessageRequestJSONContract(t *testing.T) {
	request := protocol.Message{
		Action:  "request",
		ID:      "synthetic-request",
		Channel: "synthetic-channel",
		Ticket:  "synthetic-ticket",
		TTL:     300,
		Idle:    45,
		Target:  "host.example",
		Proof:   []byte("synthetic-proof"),
		Operation: approval.Operation{
			Host:    "host.example",
			Kind:    "exec",
			Command: "printf synthetic",
			Client:  "synthetic-client",
		},
	}

	fields := marshalObject(t, request)
	requireField(t, fields, "action", "request")
	requireField(t, fields, "id", "synthetic-request")
	requireField(t, fields, "channel", "synthetic-channel")
	requireField(t, fields, "ticket", "synthetic-ticket")
	requireField(t, fields, "ttl", 300)
	requireField(t, fields, "idle", 45)
	requireField(t, fields, "target", "host.example")
	requireField(t, fields, "proof", base64.StdEncoding.EncodeToString(request.Proof))
	requireAbsent(t, fields, "request")
	requireAbsent(t, fields, "workspaceChange")
	requireAbsent(t, fields, "error")

	var operation approval.Operation
	decodeField(t, fields, "operation", &operation)
	if !reflect.DeepEqual(operation, request.Operation) {
		t.Fatalf("operation = %#v, want %#v", operation, request.Operation)
	}
	requireRoundTrip(t, request)
}

func TestMessageResponseJSONContract(t *testing.T) {
	response := protocol.Message{
		Action:        "done",
		ID:            "synthetic-response",
		Allow:         true,
		ErrorCode:     "synthetic_code",
		Error:         "synthetic failure",
		ExitCode:      7,
		MonitorActive: true,
		Phase:         "verify",
		Cols:          120,
		Rows:          40,
		Hosts:         []string{"host.example"},
		Data:          []byte("synthetic-data"),
		Transfer:      &sshconn.TransferMeta{Size: 42, Offset: 7, Hash: "synthetic-hash", PrefixHash: "synthetic-prefix"},
	}

	fields := marshalObject(t, response)
	requireField(t, fields, "action", "done")
	requireField(t, fields, "id", "synthetic-response")
	requireField(t, fields, "allow", true)
	requireField(t, fields, "errorCode", "synthetic_code")
	requireField(t, fields, "error", "synthetic failure")
	requireField(t, fields, "exitCode", 7)
	requireField(t, fields, "monitorActive", true)
	requireField(t, fields, "phase", "verify")
	requireField(t, fields, "cols", 120)
	requireField(t, fields, "rows", 40)
	requireField(t, fields, "hosts", []string{"host.example"})
	requireField(t, fields, "data", base64.StdEncoding.EncodeToString(response.Data))
	requireAbsent(t, fields, "request")
	requireAbsent(t, fields, "proof")
	requireAbsent(t, fields, "workspaceChange")

	var transfer sshconn.TransferMeta
	decodeField(t, fields, "transfer", &transfer)
	if !reflect.DeepEqual(transfer, *response.Transfer) {
		t.Fatalf("transfer = %#v, want %#v", transfer, *response.Transfer)
	}
	requireRoundTrip(t, response)
}

func TestMessageWorkspaceChangeJSONContract(t *testing.T) {
	change := protocol.WorkspaceChange{
		Kind:      "parameter-history-save",
		HostID:    "synthetic-host",
		CommandID: "synthetic-command",
		Command:   &snippet.Command{ID: "synthetic-command", Name: "Synthetic command", Body: "printf synthetic"},
		Note:      &vault.ServerNote{Manual: "synthetic note", CheckedAt: time.Date(2026, 11, 5, 0, 0, 0, 123, time.UTC)},
		Target:    "synthetic-target",
		System:    "linux",
		Inputs:    []string{"synthetic-input"},
		Parameter: 3,
	}
	message := protocol.Message{Action: "workspaceWrite", WorkspaceChange: &change}

	fields := marshalObject(t, message)
	requireField(t, fields, "action", "workspaceWrite")
	requireAbsent(t, fields, "workspace")
	requireAbsent(t, fields, "workspaceResult")

	var got protocol.WorkspaceChange
	decodeField(t, fields, "workspaceChange", &got)
	if !reflect.DeepEqual(got, change) {
		t.Fatalf("workspaceChange = %#v, want %#v", got, change)
	}
	requireRoundTrip(t, message)
}

func marshalObject(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	return fields
}

func requireField(t *testing.T, fields map[string]json.RawMessage, name string, want any) {
	t.Helper()
	raw, ok := fields[name]
	if !ok {
		t.Fatalf("missing JSON field %q", name)
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected %q: %v", name, err)
	}
	if !bytes.Equal(raw, encoded) {
		t.Fatalf("JSON field %q = %s, want %s", name, raw, encoded)
	}
}

func requireAbsent(t *testing.T, fields map[string]json.RawMessage, name string) {
	t.Helper()
	if raw, ok := fields[name]; ok {
		t.Fatalf("unexpected JSON field %q: %s", name, raw)
	}
}

func decodeField(t *testing.T, fields map[string]json.RawMessage, name string, target any) {
	t.Helper()
	raw, ok := fields[name]
	if !ok {
		t.Fatalf("missing JSON field %q", name)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode JSON field %q: %v", name, err)
	}
}

func requireRoundTrip(t *testing.T, want protocol.Message) {
	t.Helper()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal round trip: %v", err)
	}
	var got protocol.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}
