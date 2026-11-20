package accesspolicy

import "sshclient/internal/operationrisk"

// DestructiveCommand retains the persistent policy's existing best-effort guard.
func DestructiveCommand(command string) string { return operationrisk.DestructiveCommand(command) }
