package broker

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
)

// All fields except ctx/identity are protected by ownerMu. IDs locate channels;
// only the actual accepted connection can exercise its authority.
type callerChannel struct {
	id        string
	ctx       context.Context
	cancel    context.CancelFunc
	conn      net.Conn
	identity  *localipc.ProcessIdentity
	peer      approval.Peer
	bucket    string
	requests  map[string]struct{}
	session   string
	parent    *callerChannel
	operation bool
	// ownerSession is set only after a management proof verifies on this actual
	// accepted channel. It is never read from Message or approval.Request.
	ownerSession string
}

func (s *Server) PublicKeyPin() string { return s.publicKeyPin }

func (s *Server) handle(raw net.Conn) {
	defer raw.Close()
	identity, _ := localipc.Identify(raw)
	defer identity.Close()
	peer := approval.Peer{}
	bucket := ""
	if pid, err := localipc.PeerPID(raw); err == nil && pid > 0 {
		peer = s.peerProcess(pid)
		bucket = fmt.Sprintf("pid:%d", pid)
	}
	if identity != nil {
		peer.Instance, peer.CodeID, peer.Bundle = identity.Instance, identity.CodeID, identity.AppID
	}
	connection := tls.Server(raw, s.tlsConfig)
	stop := context.AfterFunc(s.ctx, func() { raw.Close() })
	defer stop()
	connection.SetDeadline(time.Now().Add(10 * time.Second))
	if err := connection.HandshakeContext(s.ctx); err != nil {
		return
	}
	id, err := localipc.ChannelBinding(connection)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	channel := &callerChannel{id: id, ctx: ctx, cancel: cancel, conn: raw, identity: identity, peer: peer, bucket: bucket, requests: make(map[string]struct{})}
	s.ownerMu.Lock()
	s.channels[id] = channel
	s.ownerMu.Unlock()
	defer func() {
		cancel()
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		delete(s.channels, id)
		for request := range channel.requests {
			r, err := s.Approvals.Get(request)
			if err != nil && request == channel.session {
				r, err = s.Approvals.SessionStatus(request)
			}
			s.Approvals.Disconnect(request)
			delete(s.requestChannels, request)
			delete(s.requestOwners, request)
			if err == nil && (r.Status == "pending" || r.Status == "approved" || r.Status == "running" || r.Status == "expired" && request == channel.session) {
				r.Status = "revoked"
				_ = s.auditRequest("revoked", &r)
			}
		}
	}()
	wire := &localipc.Wire{Conn: connection, WriteTimeout: 10 * time.Second}
	if identity != nil {
		wire.Verify = identity.Verify
	}
	for {
		var q Message
		if err := wire.Receive(&q); err != nil {
			return
		}
		if err := s.handleMessage(wire, q, channel); err != nil {
			return
		}
		s.ownerMu.Lock()
		bound := len(channel.requests) > 0
		keep := bound || channel.operation
		if bound {
			connection.SetReadDeadline(time.Time{})
		} else {
			connection.SetReadDeadline(time.Now().Add(30 * time.Second))
		}
		s.ownerMu.Unlock()
		if !keep || q.Action == "run" || q.Action == "withdraw" {
			return
		}
	}
}

func (s *Server) bindRequest(channel *callerChannel, id string) {
	channel.requests[id] = struct{}{}
	s.requestChannels[id] = channel
	channel.conn.SetReadDeadline(time.Time{})
}

func (s *Server) ownsRequest(channel *callerChannel, id string) bool {
	if channel == nil || channel.ctx.Err() != nil || s.requestChannels[id] != channel {
		return false
	}
	if channel.identity != nil && channel.identity.Verify() != nil {
		return false
	}
	return channel.parent == nil || s.ownsSession(channel.parent, channel.parent.session)
}

func (s *Server) ownsSession(channel *callerChannel, id string) bool {
	return channel != nil && id != "" && channel.session == id && channel.ctx.Err() == nil && s.requestChannels[id] == channel && channel.identity != nil && channel.identity.Verify() == nil
}

func (s *Server) checkChannels() {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	for _, channel := range s.channels {
		if channel.identity != nil && channel.identity.Verify() != nil {
			channel.cancel()
			channel.conn.Close()
			continue
		}
		for id := range channel.requests {
			r, err := s.Approvals.Get(id)
			if err != nil && id == channel.session {
				r, err = s.Approvals.SessionStatus(id)
			}
			// Let the waiting caller read a child denial. Closing the socket
			// first turns it into a transport error and ends the parent session.
			if err == nil && r.Status == "denied" && r.ParentID != "" {
				continue
			}
			if err != nil || !(r.Status == "pending" || r.Status == "approved" || r.Status == "running" || id == channel.session && r.Status == "expired") {
				channel.cancel()
				channel.conn.Close()
			}
		}
	}
}
