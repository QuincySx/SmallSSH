//go:build !linux

package systemauth

import "context"

// These platforms carry their protected device key inside the envelope.
func CleanupOrphans(context.Context, string) error { return nil }
