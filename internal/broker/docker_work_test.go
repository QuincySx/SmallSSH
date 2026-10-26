package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/serverops"
	"sshclient/internal/testdocker"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func dockerBrokerFixture(t *testing.T) (client.Client, *testdocker.Fixture) {
	t.Helper()
	f := testdocker.New(t)
	ssh := testserver.NewWithExec(t, f.Execute)
	dir := dockerPrivateDir(t)
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(ssh.Profile())
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	s, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return client.Client{Dir: dir}, f
}

func dockerPrivateDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
func approvedDockerStream(t *testing.T, c client.Client, op approval.Operation) *client.Stream {
	t.Helper()
	op.Host, op.Client = "fixture", "Docker QA"
	r, ticket := request(t, c, op)
	if stream, err := c.Start(context.Background(), r.ID, ticket, 80, 24); err == nil {
		stream.Close()
		t.Fatal("unapproved Docker request executed")
	}
	approve(t, c, r)
	stream, err := c.Start(context.Background(), r.ID, ticket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func TestComposeWorkflowApprovalsPlanPrivacyAndFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"success", "pull-fail", "change", "up-fail", "bad-status", "stale"} {
		t.Run(mode, func(t *testing.T) {
			c, f := dockerBrokerFixture(t)
			if err := os.WriteFile(f.State, []byte(mode), 0600); err != nil {
				t.Fatal(err)
			}
			stream := approvedDockerStream(t, c, approval.Operation{Kind: "compose-plan", Compose: f.Target})
			var plan *serverops.ComposePlan
			for {
				m, err := stream.Receive()
				if err != nil {
					t.Fatal(err)
				}
				if m.ComposePlan != nil {
					plan = m.ComposePlan
				}
				if m.Action == "done" {
					break
				}
			}
			stream.Close()
			if plan == nil || plan.Hash == "" {
				t.Fatal("missing plan")
			}
			data, _ := json.Marshal(plan)
			if bytes.Contains(data, []byte("fixture-private-value")) {
				t.Fatal("plan leaked environment")
			}
			r := f.Target
			r.ConfigHash = plan.Hash
			if mode == "stale" {
				r.ConfigHash = strings.Repeat("0", 64)
			}
			stream = approvedDockerStream(t, c, approval.Operation{Kind: "compose-update", Compose: r})
			defer stream.Close()
			var phases []string
			var out strings.Builder
			var err error
			var rows []serverops.ComposeStatus
			for {
				m, e := stream.Receive()
				if e != nil {
					err = e
					break
				}
				if m.Action == "done" {
					break
				}
				if m.Phase != "" {
					phases = append(phases, m.Phase)
				}
				out.Write(m.Data)
				if m.ComposeStatus != nil {
					rows = m.ComposeStatus
				}
			}
			calls, _ := os.ReadFile(f.Calls)
			if mode == "success" {
				if err != nil || strings.Join(phases, ",") != "pull,up,verify" || len(rows) != 1 || !strings.Contains(out.String(), "pulling") {
					t.Fatal(phases, rows, err, out.String())
				}
			} else {
				if err == nil {
					t.Fatal("failed update reported success", mode)
				}
				if mode == "stale" || mode == "pull-fail" || mode == "change" {
					if strings.Contains(string(calls), "up up") {
						t.Fatal("failed preflight/pull reached rebuild", string(calls))
					}
				}
			}
			if strings.Contains(out.String(), "fixture-private-value") {
				t.Fatal("model entered task log")
			}
		})
	}
}

func TestDockerLogsUseSharedApprovalAndReturnBothStreams(t *testing.T) {
	c, f := dockerBrokerFixture(t)
	stream := approvedDockerStream(t, c, approval.Operation{Kind: "docker-logs", Docker: serverops.DockerRequest{Target: f.Target.Target, Tail: 200}})
	defer stream.Close()
	var out, stderr bytes.Buffer
	code, _, err := stream.Copy(&out, &stderr)
	if err != nil || code != 0 || !strings.Contains(out.String(), "fixture log") || !strings.Contains(stderr.String(), "fixture stderr") {
		t.Fatal(code, err, out.String(), stderr.String())
	}
}

func TestContainerTerminalPTYInputResizeAndDisconnect(t *testing.T) {
	f := testdocker.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ssh := testserver.NewWithStreamExec(t, func(sessionCtx context.Context, command string, in io.Reader, out, stderr io.Writer) int {
		return f.Stream(sessionCtx, command, in, out, stderr)
	})
	dir := dockerPrivateDir(t)
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	localipc.Prepare(dir)
	v, _ := vault.New(pass)
	v.Put(ssh.Profile())
	vault.Save(broker.VaultPath(dir), v, true)
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	c := client.Client{Dir: dir}
	stream := approvedDockerStream(t, c, approval.Operation{Kind: "docker-terminal", Docker: serverops.DockerRequest{Target: f.Target.Target, Shell: "sh"}})
	defer stream.Close()
	reader := &client.TerminalConn{Stream: stream}
	buf := make([]byte, 128)
	n, err := reader.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "container-ready") {
		t.Fatal(string(buf[:n]), err)
	}
	if err = stream.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]uint32{{80, 24}, {120, 40}} {
		select {
		case actual := <-ssh.Sizes:
			if actual != size {
				t.Fatal(actual, size)
			}
		case <-ctx.Done():
			t.Fatal("missing PTY resize")
		}
	}
	if _, err = stream.Write([]byte("中文\x1b[A\x03\n")); err != nil {
		t.Fatal(err)
	}
	n, err = reader.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "中文") {
		t.Fatal(string(buf[:n]), err)
	}
	stream.Close()
}
