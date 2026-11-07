package service

import "sshclient/internal/broker"

func PendingOwnersForTesting(h *Host) int {
	if h == nil {
		return 0
	}
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	return h.pendingOwners
}

func CoreForTesting(h *Host) *broker.Server {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.core
}
