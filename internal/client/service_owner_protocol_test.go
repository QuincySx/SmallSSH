package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

const (
	syntheticOwnerPendingID = "00112233445566778899aabbccddeeff"
	syntheticOwnerSessionID = "fedcba9876543210fedcba9876543210"
)

var syntheticOwnerBrokerKey = "sha256:" + strings.Repeat("0123456789abcdef", 4)

func newSyntheticOwnerEndpoint(t *testing.T, handler func(*localipc.Wire, protocol.ServiceMessage, protocol.ServiceMessage) error) *syntheticPrepareEndpoint {
	t.Helper()
	return newSyntheticPrepareEndpoint(t, func(wire *localipc.Wire) error {
		hello, commit, err := syntheticOwnerCommitDialogue(wire)
		if err != nil {
			return err
		}
		return handler(wire, hello, commit)
	})
}

func syntheticOwnerCommitDialogue(wire *localipc.Wire) (protocol.ServiceMessage, protocol.ServiceMessage, error) {
	var helloRequest protocol.ServiceMessage
	if err := syntheticPrepareReceive(wire, &helloRequest); err != nil {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, err
	}
	if helloRequest.Action != "hello" {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, errors.New("synthetic endpoint did not receive hello")
	}

	hello := syntheticPrepareHello()
	if err := syntheticPrepareSend(wire, hello); err != nil {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, err
	}

	var prepare protocol.ServiceMessage
	if err := syntheticPrepareReceive(wire, &prepare); err != nil {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, err
	}
	defer clearServiceMessageSecrets(&prepare)
	if prepare.Action != "prepareOwner" ||
		prepare.Major != protocol.ServiceProtocolMajor ||
		prepare.Minor != hello.Minor ||
		prepare.Epoch != hello.Epoch ||
		prepare.Challenge != hello.Challenge ||
		prepare.Generation != hello.Generation ||
		prepare.OwnerPrepare == nil ||
		!bytes.Equal(prepare.UnlockKey, bytes.Repeat([]byte{0x5a}, 32)) ||
		!bytes.Equal(prepare.Proof, syntheticPreparePublicBytes()) {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, errors.New("synthetic endpoint did not receive the public prepare frame")
	}

	if err := syntheticPrepareSend(wire, syntheticPreparePrepared(hello, syntheticOwnerPendingID)); err != nil {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, err
	}

	var commit protocol.ServiceMessage
	if err := syntheticPrepareReceive(wire, &commit); err != nil {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, err
	}
	defer clearServiceMessageSecrets(&commit)
	if commit.Action != "commitOwner" ||
		commit.Major != protocol.ServiceProtocolMajor ||
		commit.Minor != hello.Minor ||
		commit.Epoch != hello.Epoch ||
		commit.Generation != hello.Generation ||
		commit.ID != syntheticOwnerPendingID ||
		commit.Challenge != "" ||
		commit.OwnerPrepare != nil ||
		commit.OwnerInfo != nil ||
		commit.Status != nil ||
		len(commit.UnlockKey) != 0 ||
		len(commit.Proof) != 0 {
		return protocol.ServiceMessage{}, protocol.ServiceMessage{}, errors.New("synthetic endpoint did not receive a public commit frame")
	}
	return hello, commit, nil
}

func syntheticOwnerCommitted(hello protocol.ServiceMessage, expires time.Time) (protocol.ServiceMessage, error) {
	response := protocol.ServiceMessage{
		Action:     "ownerCommitted",
		Major:      protocol.ServiceProtocolMajor,
		Minor:      hello.Minor,
		Epoch:      hello.Epoch,
		Generation: hello.Generation,
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return protocol.ServiceMessage{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return protocol.ServiceMessage{}, err
	}
	ownerInfo, err := json.Marshal(map[string]any{
		"id":        syntheticOwnerSessionID,
		"epoch":     hello.Epoch,
		"expires":   expires,
		"brokerKey": syntheticOwnerBrokerKey,
	})
	if err != nil {
		return protocol.ServiceMessage{}, err
	}
	fields["ownerInfo"] = ownerInfo
	encoded, err = json.Marshal(fields)
	if err != nil {
		return protocol.ServiceMessage{}, err
	}
	var committed protocol.ServiceMessage
	if err := json.Unmarshal(encoded, &committed); err != nil {
		return protocol.ServiceMessage{}, err
	}
	if committed.OwnerInfo == nil {
		return protocol.ServiceMessage{}, errors.New("synthetic owner info field unavailable")
	}
	return committed, nil
}

func syntheticOwnerInjectStatus(response *protocol.ServiceMessage) error {
	var extension protocol.ServiceMessage
	if err := json.Unmarshal([]byte(`{"status":{}}`), &extension); err != nil {
		return err
	}
	if extension.Status == nil {
		return errors.New("synthetic status field unavailable")
	}
	response.Status = extension.Status
	return nil
}

func syntheticOwnerWrongTypedResponse(hello protocol.ServiceMessage, expires time.Time) (json.RawMessage, error) {
	response, err := syntheticOwnerCommitted(hello, expires)
	if err != nil {
		return nil, err
	}
	response.UnlockKey = []byte{0x5a}
	response.Proof = []byte{0x5a}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	ordered := struct {
		Action     json.RawMessage `json:"action"`
		Major      json.RawMessage `json:"major"`
		Minor      json.RawMessage `json:"minor"`
		Epoch      json.RawMessage `json:"epoch"`
		Generation json.RawMessage `json:"generation"`
		OwnerInfo  json.RawMessage `json:"ownerInfo"`
		UnlockKey  json.RawMessage `json:"unlockKey"`
		Proof      json.RawMessage `json:"proof"`
		Status     json.RawMessage `json:"status"`
	}{
		Action:     fields["action"],
		Major:      fields["major"],
		Minor:      fields["minor"],
		Epoch:      fields["epoch"],
		Generation: fields["generation"],
		OwnerInfo:  fields["ownerInfo"],
		UnlockKey:  fields["unlockKey"],
		Proof:      fields["proof"],
		Status:     json.RawMessage(`7`),
	}
	encoded, err = json.Marshal(ordered)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func syntheticOwnerRequirePeerClosed(wire *localipc.Wire, timeout time.Duration) error {
	if err := wire.Conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	var message protocol.ServiceMessage
	defer clearServiceMessageSecrets(&message)
	err := wire.Receive(&message)
	if err == nil {
		return errors.New("synthetic client sent an unexpected frame after a terminal response")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New("synthetic client left the control connection open")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		return errors.New("synthetic control connection failed before the client closed it")
	}
	return nil
}

func syntheticOwnerWait(t *testing.T, endpoint *syntheticPrepareEndpoint, timeout time.Duration) {
	t.Helper()
	select {
	case <-endpoint.done:
	case <-time.After(timeout):
		t.Fatal("synthetic owner endpoint did not finish in time")
	}
	if err := <-endpoint.result; err != nil {
		t.Fatalf("synthetic owner endpoint failed: %v", err)
	}
}

func syntheticOwnerAssertSourceCleared(t *testing.T, source *syntheticPrepareSource) {
	t.Helper()
	unlockCalls, proofCalls := source.calls()
	if unlockCalls != 1 || proofCalls != 1 {
		t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
	}
	unlockCleared, proofCleared := source.buffersCleared()
	if !unlockCleared || !proofCleared {
		t.Fatal("synthetic source buffers were not cleared")
	}
}

func TestServiceOwnerCommittedRejectsInvalidPublicFields(t *testing.T) {
	const lifetime = 2 * time.Minute
	tests := []struct {
		name   string
		mutate func(*protocol.ServiceMessage, time.Duration) error
	}{
		{
			name: "wrong action",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Action = "ownerPrepared"
				return nil
			},
		},
		{
			name: "wrong major version",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Major++
				return nil
			},
		},
		{
			name: "wrong minor version",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Minor++
				return nil
			},
		},
		{
			name: "wrong epoch",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Epoch = strings.Repeat("0", 32)
				return nil
			},
		},
		{
			name: "wrong generation",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Generation++
				return nil
			},
		},
		{
			name: "missing owner info",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.OwnerInfo = nil
				return nil
			},
		},
		{
			name: "invalid owner id",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.OwnerInfo.ID = strings.Repeat("a", 31)
				return nil
			},
		},
		{
			name: "invalid owner epoch",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.OwnerInfo.Epoch = strings.Repeat("A", 32)
				return nil
			},
		},
		{
			name: "expired owner info",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.OwnerInfo.Expires = time.Now().Add(-time.Second)
				return nil
			},
		},
		{
			name: "excessive owner lifetime",
			mutate: func(response *protocol.ServiceMessage, requested time.Duration) error {
				response.OwnerInfo.Expires = time.Now().Add(requested + 6*time.Second)
				return nil
			},
		},
		{
			name: "invalid broker key pin",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.OwnerInfo.BrokerKey = "sha256:" + strings.Repeat("g", 64)
				return nil
			},
		},
		{
			name: "extra owner prepare",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.OwnerPrepare = &protocol.OwnerPrepare{}
				return nil
			},
		},
		{
			name: "extra status",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				return syntheticOwnerInjectStatus(response)
			},
		},
		{
			name: "extra challenge",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Challenge = strings.Repeat("0", 32)
				return nil
			},
		},
		{
			name: "extra id",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.ID = syntheticOwnerSessionID
				return nil
			},
		},
		{
			name: "extra unlock key",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.UnlockKey = []byte{0x5a}
				return nil
			},
		},
		{
			name: "extra proof",
			mutate: func(response *protocol.ServiceMessage, _ time.Duration) error {
				response.Proof = []byte{0x5a}
				return nil
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			endpoint := newSyntheticOwnerEndpoint(t, func(wire *localipc.Wire, hello protocol.ServiceMessage, _ protocol.ServiceMessage) error {
				response, err := syntheticOwnerCommitted(hello, time.Now().Add(lifetime))
				if err != nil {
					return err
				}
				if err := test.mutate(&response, lifetime); err != nil {
					return err
				}
				if err := syntheticPrepareSend(wire, response); err != nil {
					return err
				}
				return syntheticOwnerRequirePeerClosed(wire, 5*time.Second)
			})
			source := &syntheticPrepareSource{}
			prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, lifetime)
			if err != nil {
				t.Fatalf("PrepareOwner failed: %v", err)
			}
			if prepared == nil {
				t.Fatal("PrepareOwner returned no prepared owner")
			}
			t.Cleanup(prepared.Close)

			session, err := prepared.Commit(syntheticPrepareContext(t))
			if session != nil {
				session.Close()
				t.Fatal("Commit returned an owner session for an invalid public response")
			}
			if err == nil {
				t.Fatal("Commit accepted an invalid public response")
			}
			select {
			case <-prepared.Done():
			default:
				t.Fatal("prepared owner remained open after an invalid commit response")
			}
			syntheticOwnerWait(t, endpoint, 5*time.Second)
			syntheticOwnerAssertSourceCleared(t, source)
		})
	}
}

func TestServiceOwnerCommittedRejectsWrongTypedJSON(t *testing.T) {
	const lifetime = 2 * time.Minute
	endpoint := newSyntheticOwnerEndpoint(t, func(wire *localipc.Wire, hello protocol.ServiceMessage, _ protocol.ServiceMessage) error {
		response, err := syntheticOwnerWrongTypedResponse(hello, time.Now().Add(lifetime))
		if err != nil {
			return err
		}
		if err := syntheticPrepareSend(wire, response); err != nil {
			return err
		}
		return syntheticOwnerRequirePeerClosed(wire, 5*time.Second)
	})
	source := &syntheticPrepareSource{}
	prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, lifetime)
	if err != nil {
		t.Fatalf("PrepareOwner failed: %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner returned no prepared owner")
	}
	t.Cleanup(prepared.Close)

	session, err := prepared.Commit(syntheticPrepareContext(t))
	if session != nil {
		session.Close()
		t.Fatal("Commit returned an owner session for wrong-typed JSON")
	}
	if err == nil {
		t.Fatal("Commit accepted wrong-typed JSON with public placeholders")
	}
	select {
	case <-prepared.Done():
	default:
		t.Fatal("prepared owner remained open after wrong-typed JSON")
	}
	syntheticOwnerWait(t, endpoint, 5*time.Second)
	syntheticOwnerAssertSourceCleared(t, source)
}

func TestServiceOwnerCommittedAcceptsDelayedResponse(t *testing.T) {
	const lifetime = 2 * time.Minute
	registered := make(chan time.Time, 1)
	endpoint := newSyntheticOwnerEndpoint(t, func(wire *localipc.Wire, hello protocol.ServiceMessage, _ protocol.ServiceMessage) error {
		if err := wire.Conn.SetDeadline(time.Now().Add(9 * time.Second)); err != nil {
			return err
		}
		time.Sleep(6 * time.Second)
		expires := time.Now().Add(lifetime)
		response, err := syntheticOwnerCommitted(hello, expires)
		if err != nil {
			return err
		}
		registered <- expires
		if err := wire.Send(response); err != nil {
			return err
		}
		return syntheticOwnerRequirePeerClosed(wire, 5*time.Second)
	})
	source := &syntheticPrepareSource{}
	prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, lifetime)
	if err != nil {
		t.Fatalf("PrepareOwner failed: %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner returned no prepared owner")
	}
	t.Cleanup(prepared.Close)

	commitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := prepared.Commit(commitCtx)
	if err != nil {
		t.Fatalf("Commit failed for a delayed valid response: %v", err)
	}
	if session == nil {
		t.Fatal("Commit returned no owner session for a delayed valid response")
	}
	t.Cleanup(session.Close)

	var expires time.Time
	select {
	case expires = <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("delayed endpoint did not publish its registered lifetime")
	}
	if !session.Info().Expires.Equal(expires) {
		t.Fatal("owner session did not preserve the lifetime registered after the delay")
	}
	pin, err := session.BrokerPublicKeyPin()
	if err != nil || pin != syntheticOwnerBrokerKey {
		t.Fatal("owner session did not preserve the public broker key pin")
	}
	session.Close()
	syntheticOwnerWait(t, endpoint, 5*time.Second)
	syntheticOwnerAssertSourceCleared(t, source)
}

func TestServiceOwnerHeartbeatRejectsWrongHeader(t *testing.T) {
	const lifetime = 2 * time.Minute
	heartbeatResult := make(chan error, 1)
	endpoint := newSyntheticOwnerEndpoint(t, func(wire *localipc.Wire, hello protocol.ServiceMessage, commit protocol.ServiceMessage) error {
		response, err := syntheticOwnerCommitted(hello, time.Now().Add(lifetime))
		if err != nil {
			return err
		}
		if err := syntheticPrepareSend(wire, response); err != nil {
			return err
		}
		if err := wire.Conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
			heartbeatResult <- err
			return err
		}
		var heartbeat protocol.ServiceMessage
		if err := wire.Receive(&heartbeat); err != nil {
			heartbeatResult <- err
			return err
		}
		hasSecrets := len(heartbeat.UnlockKey) != 0 || len(heartbeat.Proof) != 0
		clearServiceMessageSecrets(&heartbeat)
		if heartbeat.Action != "heartbeat" ||
			heartbeat.Major != protocol.ServiceProtocolMajor ||
			heartbeat.Minor != hello.Minor ||
			heartbeat.Epoch != hello.Epoch ||
			heartbeat.Generation != commit.Generation ||
			heartbeat.Challenge != "" ||
			heartbeat.ID != "" ||
			heartbeat.OwnerPrepare != nil ||
			heartbeat.OwnerInfo != nil ||
			heartbeat.Status != nil ||
			heartbeat.Error != "" ||
			heartbeat.ErrorCode != "" ||
			hasSecrets {
			err := errors.New("synthetic endpoint received an invalid public heartbeat")
			heartbeatResult <- err
			return err
		}
		sendErr := wire.Send(protocol.ServiceMessage{
			Action:     "heartbeat",
			Major:      protocol.ServiceProtocolMajor,
			Minor:      hello.Minor,
			Epoch:      hello.Epoch,
			Generation: heartbeat.Generation + 1,
		})
		heartbeatResult <- sendErr
		if sendErr != nil {
			return sendErr
		}
		return syntheticOwnerRequirePeerClosed(wire, 5*time.Second)
	})
	source := &syntheticPrepareSource{}
	prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, lifetime)
	if err != nil {
		t.Fatalf("PrepareOwner failed: %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner returned no prepared owner")
	}
	t.Cleanup(prepared.Close)

	session, err := prepared.Commit(syntheticPrepareContext(t))
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if session == nil {
		t.Fatal("Commit returned no owner session")
	}
	t.Cleanup(session.Close)

	select {
	case err := <-heartbeatResult:
		if err != nil {
			t.Fatalf("synthetic heartbeat exchange failed: %v", err)
		}
	case <-time.After(16 * time.Second):
		t.Fatal("synthetic endpoint did not receive a heartbeat")
	}
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("owner session did not close within two seconds of a rejected heartbeat")
	}
	proof, err := session.Proof([]byte("synthetic public heartbeat proof payload"))
	if err == nil || len(proof) != 0 {
		t.Fatal("owner session still produced a proof after a rejected heartbeat")
	}
	session.Close()
	syntheticOwnerWait(t, endpoint, 5*time.Second)
	syntheticOwnerAssertSourceCleared(t, source)
}

func TestServiceOwnerSessionRejectsUnsolicitedFrame(t *testing.T) {
	const lifetime = 2 * time.Minute
	endpoint := newSyntheticOwnerEndpoint(t, func(wire *localipc.Wire, hello protocol.ServiceMessage, commit protocol.ServiceMessage) error {
		response, err := syntheticOwnerCommitted(hello, time.Now().Add(lifetime))
		if err != nil {
			return err
		}
		if err := syntheticPrepareSend(wire, response); err != nil {
			return err
		}
		if err := syntheticPrepareSend(wire, protocol.ServiceMessage{
			Action:     "heartbeat",
			Major:      protocol.ServiceProtocolMajor,
			Minor:      hello.Minor,
			Epoch:      hello.Epoch,
			Generation: commit.Generation,
		}); err != nil {
			return err
		}
		return syntheticOwnerRequirePeerClosed(wire, 5*time.Second)
	})
	source := &syntheticPrepareSource{}
	prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, lifetime)
	if err != nil {
		t.Fatalf("PrepareOwner failed: %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner returned no prepared owner")
	}
	t.Cleanup(prepared.Close)

	session, err := prepared.Commit(syntheticPrepareContext(t))
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if session == nil {
		t.Fatal("Commit returned no owner session")
	}
	t.Cleanup(session.Close)

	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("owner session did not close on an unsolicited frame")
	}
	proof, err := session.Proof([]byte("synthetic public unsolicited proof payload"))
	if err == nil || len(proof) != 0 {
		t.Fatal("owner session still produced a proof after an unsolicited frame")
	}
	session.Close()
	syntheticOwnerWait(t, endpoint, 5*time.Second)
	syntheticOwnerAssertSourceCleared(t, source)
}
