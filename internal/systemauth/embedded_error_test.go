package systemauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedNativeErrorClassification(t *testing.T) {
	for _, code := range []string{"cancelled", "notReady", "authenticationFailed", "biometryUnavailable", "biometryLocked", "keyUnavailable", "invalidData"} {
		t.Run(code, func(t *testing.T) {
			output, err := json.Marshal(map[string]any{
				"key": bytes.Repeat([]byte{23}, 32), "error": "指纹验证未完成", "errorCode": code,
				"nativeDomain": "LAErrorDomain", "nativeCode": -3,
			})
			if err != nil {
				t.Fatal(err)
			}
			b := &fakeEmbeddedBridge{t: t, result: output}
			key, err := runEmbedded(context.Background(), fakePresentation(), b, []byte(`{"action":"unwrap"}`))
			if key != nil || err == nil {
				t.Fatal("native failure released a key")
			}
			wrapped := fmt.Errorf("verification: %w", err)
			var native *EmbeddedError
			if !errors.As(wrapped, &native) || native.Code != code || native.Message != "指纹验证未完成" || native.NativeDomain != "LAErrorDomain" || native.NativeCode != -3 {
				t.Fatalf("native outcome was lost: %#v, %v", native, err)
			}
			if err.Error() != "指纹验证未完成" || AuthenticationErrorCode(wrapped) != code {
				t.Fatal("native message or classification changed", err)
			}
			if IsAuthenticationCancelled(wrapped) != (code == "cancelled") || errors.Is(wrapped, context.Canceled) != (code == "cancelled") {
				t.Fatal("native cancellation classification changed", err)
			}
			if IsAuthenticationNotReady(wrapped) != (code == "notReady") || IsAuthenticationUnavailable(wrapped) != (code == "biometryUnavailable") {
				t.Fatal("native readiness classification changed", err)
			}
			if b.starts != 1 || b.cancels != 1 || b.releases != 1 || b.active {
				t.Fatal("native error retried or leaked its token")
			}
			if !bytes.Equal(b.output, make([]byte, len(b.output))) {
				t.Fatal("native error output was not wiped")
			}
		})
	}
}

func TestEmbeddedLegacyAndUnknownErrorsStayFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code, message string
		wantTyped           bool
	}{
		{"legacy", "", "legacy failure", false},
		{"unknown-code", "futureOutcome", "future failure", true},
		{"unknown-code-without-message", "futureOutcome", "", true},
		{"whitespace-code", " notReady ", "", true},
		{"known-code-without-message", "notReady", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := map[string]any{"key": bytes.Repeat([]byte{31}, 32), "error": tc.message}
			if tc.code != "" {
				reply["errorCode"] = tc.code
			}
			output, err := json.Marshal(reply)
			if err != nil {
				t.Fatal(err)
			}
			key, err := embeddedKey(output)
			if key != nil || err == nil || err.Error() == "" {
				t.Fatal("incomplete authentication accepted")
			}
			var native *EmbeddedError
			if errors.As(err, &native) != tc.wantTyped {
				t.Fatal("legacy compatibility or typed error was lost", err)
			}
			wantCode := ""
			if tc.code == "notReady" {
				wantCode = tc.code
			}
			if AuthenticationErrorCode(err) != wantCode || IsAuthenticationNotReady(err) != (wantCode == "notReady") || IsAuthenticationUnavailable(err) || IsAuthenticationCancelled(err) {
				t.Fatal("legacy or unknown outcome became a classified native failure", err)
			}
		})
	}
	for _, field := range []any{nil, "", true, 1, []string{"notReady"}, map[string]string{"code": "notReady"}} {
		output, err := json.Marshal(map[string]any{"key": bytes.Repeat([]byte{31}, 32), "errorCode": field})
		if err != nil {
			t.Fatal(err)
		}
		if key, err := embeddedKey(output); key != nil || err == nil || AuthenticationErrorCode(err) != "" {
			t.Fatal("malformed native error code accepted")
		}
	}
}

func TestSystemOpenPreservesContextFailure(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(record{Version: 1, Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path(dir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, deadline := range []bool{false, true} {
		for _, lateKey := range []bool{false, true} {
			t.Run(fmt.Sprintf("deadline=%v/late-key=%v", deadline, lateKey), func(t *testing.T) {
				var ctx context.Context
				var cancel context.CancelFunc
				want := context.Canceled
				if deadline {
					ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
					want = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				calls := 0
				s := &Service{}
				session, err := s.open(ctx, dir, "test", func(context.Context, json.RawMessage, string) ([]byte, error) {
					calls++
					cancel()
					if lateKey {
						return bytes.Repeat([]byte{19}, 32), nil
					}
					return nil, errors.New("native verification did not complete")
				})
				if session != nil || err == nil || !errors.Is(err, want) || !strings.HasPrefix(err.Error(), "系统验证已取消或超时") || calls != 1 {
					t.Fatalf("context failure was hidden or accepted: session=%v err=%v calls=%d", session, err, calls)
				}
				if IsAuthenticationCancelled(err) != !deadline || IsAuthenticationNotReady(err) || IsAuthenticationUnavailable(err) {
					t.Fatal("context cancellation became a hardware failure", err)
				}
			})
		}
	}
}

func TestEmbeddedServiceCancellationPrecedesReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &fakeEmbeddedBackend{}
	s := &Service{backend: b}
	session, err := s.OpenEmbedded(ctx, t.TempDir(), "test", nil)
	if session != nil || !errors.Is(err, context.Canceled) || IsAuthenticationNotReady(err) || b.calls != 0 {
		t.Fatal("cancelled operation was classified as a startup failure", err)
	}
}
