package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/broker"
	"sshclient/internal/client"
)

func TestCommandSessionRunsSeveralLinesWithOneApprovalAndRevokesOnExit(t *testing.T) {
	for _, ending := range []string{"", ":quit\nmust-not-run\n"} {
		t.Run(ending, func(t *testing.T) {
			c, ctx := sessionFixture(t)
			var out, notices bytes.Buffer
			code := -1
			done := make(chan error, 1)
			go func() {
				var err error
				code, err = execSession(ctx, c, "command fixture", []string{"fixture", "2m", "1m"}, strings.NewReader("first\n\nsecond\n"+ending), &out, &notices)
				done <- err
			}()
			parent := approveCLISession(t, ctx, c, done)
			if parent.Operation.Grant.Permission != "full" || parent.Operation.Grant.LifetimeSeconds != 120 || parent.Operation.Grant.IdleSeconds != 60 {
				t.Fatal("command session did not request the reviewed command scope", parent)
			}
			select {
			case err := <-done:
				if err != nil || code != 0 {
					t.Fatal(code, err)
				}
			case <-ctx.Done():
				t.Fatal("another line requested another approval", ctx.Err())
			}
			if !strings.Contains(out.String(), "first") || !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "must-not-run") {
				t.Fatal("commands were lost, replayed after quit or encoded as JSON", out.String())
			}
			if strings.Count(notices.String(), "再批准会话") != 1 || strings.Count(notices.String(), "[退出码 0]") != 2 {
				t.Fatal("consent or command boundaries were repeated or lost", notices.String())
			}
			waitCommandSessionRevoked(t, ctx, c, parent.ID)
		})
	}
}

type commandNoticeWriter func([]byte) (int, error)

func (f commandNoticeWriter) Write(p []byte) (int, error) { return f(p) }

func TestCommandSessionRevocationDoesNotFallBackToSingleApproval(t *testing.T) {
	c, ctx := sessionFixture(t)
	input, feed := io.Pipe()
	t.Cleanup(func() { input.Close(); feed.Close() })
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	var out bytes.Buffer
	notices := commandNoticeWriter(func(p []byte) (int, error) {
		if bytes.Contains(p, []byte("会话已批准")) {
			ready <- struct{}{}
		}
		return len(p), nil
	})
	go func() {
		_, err := execSession(ctx, c, "revoked fixture", []string{"fixture"}, input, &out, notices)
		done <- err
	}()
	parent := approveCLISession(t, ctx, c, done)
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := c.DecideWithOwner(ctx, parent.ID, false, true, c.Owner); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.WriteString(feed, "must-not-run\n") }()
	select {
	case err := <-done:
		if err == nil || out.Len() != 0 {
			t.Fatal("revoked session executed or reopened", out.String(), err)
		}
	case <-ctx.Done():
		t.Fatal("revoked session waited for another consent", ctx.Err())
	}
	pending, err := c.Call(ctx, broker.Message{Action: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range pending.Requests {
		if request.Status == "pending" {
			t.Fatal("revocation silently opened a new one-shot request", request)
		}
	}
}

func TestCommandSessionRejectsBadTermsBeforeRequestingConsent(t *testing.T) {
	for _, args := range [][]string{nil, {"fixture", "bad-duration"}, {"fixture", "1h", "1m", "extra"}, {"--read-root", "/srv"}} {
		code, err := execSession(context.Background(), client.Client{}, "fixture", args, strings.NewReader("must-not-run"), io.Discard, io.Discard)
		if code != 2 || err == nil {
			t.Fatal(args, code, err)
		}
	}
}

func TestCommandSessionPolicyRefusalKeepsOriginalGrantForNextLine(t *testing.T) {
	c, ctx := sessionFixtureWithPolicy(t, accesspolicy.Policy{})
	var out, notices bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := execSession(ctx, c, "policy fixture", []string{"fixture"}, strings.NewReader("rm -rf /etc\nsecond\n"), &out, &notices)
		done <- err
	}()
	parent := approveCLISession(t, ctx, c, done)
	select {
	case err := <-done:
		if err != nil || strings.Contains(out.String(), "rm -rf") || !strings.Contains(out.String(), "second") || !strings.Contains(notices.String(), "命令未执行") {
			t.Fatal("refusal widened, ended or retried the session", err, out.String(), notices.String())
		}
	case <-ctx.Done():
		t.Fatal("refused command created another approval", ctx.Err())
	}
	waitCommandSessionRevoked(t, ctx, c, parent.ID)
}

func TestCommandSessionChecksCommandLengthBeforeExecution(t *testing.T) {
	for _, size := range []int{32768, 32769} {
		label := "at-limit"
		if size > 32768 {
			label = "over-limit"
		}
		t.Run(label, func(t *testing.T) {
			c, ctx := sessionFixture(t)
			var out, notices bytes.Buffer
			done := make(chan error, 1)
			go func() {
				_, err := execSession(ctx, c, "length fixture", []string{"fixture"}, strings.NewReader(strings.Repeat("x", size)+"\n"), &out, &notices)
				done <- err
			}()
			parent := approveCLISession(t, ctx, c, done)
			select {
			case err := <-done:
				if size == 32768 && (err != nil || out.Len() < size) {
					t.Fatal("allowed boundary command did not execute", err, out.Len())
				}
				if size == 32769 && (err == nil || out.Len() != 0) {
					t.Fatal("oversized command reached remote execution", err, out.Len())
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			waitCommandSessionRevoked(t, ctx, c, parent.ID)
		})
	}
}

func waitCommandSessionRevoked(t *testing.T, ctx context.Context, c client.Client, id string) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		status, err := c.Call(ctx, broker.Message{Action: "status", ID: id})
		if err == nil && status.Request != nil && status.Request.Status == "revoked" {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("finished command session remained live", ctx.Err())
		case <-tick.C:
		}
	}
}
