package systemauth

import (
	"context"
	"errors"
)

// EmbeddedError preserves the native authentication outcome without exposing
// private native error details or treating an incomplete check as success.
type EmbeddedError struct {
	Code         string
	Message      string
	NativeDomain string
	NativeCode   int64
}

func (e *EmbeddedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "窗口内指纹验证未完成"
}

// Unwrap lets cancellation remain recognizable through ordinary error wrappers.
func (e *EmbeddedError) Unwrap() error {
	if e.Code == "cancelled" {
		return context.Canceled
	}
	return nil
}

// AuthenticationErrorCode returns a known native outcome, or an empty string
// for legacy and unrecognized errors. Unknown outcomes must not trigger retry.
func AuthenticationErrorCode(err error) string {
	var embedded *EmbeddedError
	if errors.As(err, &embedded) && embedded != nil {
		switch embedded.Code {
		case "cancelled", "notReady", "authenticationFailed", "biometryUnavailable", "biometryLocked", "keyUnavailable", "invalidData":
			return embedded.Code
		}
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return ""
}

func IsAuthenticationCancelled(err error) bool {
	return AuthenticationErrorCode(err) == "cancelled"
}

func IsAuthenticationNotReady(err error) bool {
	return AuthenticationErrorCode(err) == "notReady"
}

func IsAuthenticationUnavailable(err error) bool {
	return AuthenticationErrorCode(err) == "biometryUnavailable"
}
