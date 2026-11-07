package localipc

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"net"
	"sync"
	"time"
)

// ConnectExecutable authenticates the local server against a trusted executable
// file instead of a pin learned from the endpoint. This authenticates the code,
// not the user or owner.
func ConnectExecutable(ctx context.Context, dir, executable string) (*Wire, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := ExecutableCodeID(executable)
	if err != nil {
		return nil, err
	}
	raw, err := Dial(ctx, dir)
	if err != nil {
		return nil, &executableDialError{err: err}
	}
	identity, err := IdentifyServer(raw)
	if err != nil {
		raw.Close()
		return nil, err
	}
	closeBeforeReturn := func() {
		identity.Close()
		_ = raw.Close()
	}
	if err := verifyServerCode(identity, expected); err != nil {
		closeBeforeReturn()
		return nil, err
	}
	verify := func() error { return verifyServerCode(identity, expected) }
	connection := tls.Client(raw, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		// The self-signed certificate is not the identity anchor. The operating
		// system has already tied this exact connection to the expected program.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 {
				return ErrBrokerIdentity
			}
			certificate := state.PeerCertificates[0]
			if time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
				return ErrBrokerIdentity
			}
			return verify()
		},
	})
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		closeBeforeReturn()
		return nil, err
	}
	if err := connection.HandshakeContext(ctx); err != nil {
		closeBeforeReturn()
		return nil, err
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		closeBeforeReturn()
		return nil, err
	}
	if err := verify(); err != nil {
		closeBeforeReturn()
		return nil, err
	}
	return &Wire{
		Conn:         &executableServerConn{Conn: connection, raw: raw, identity: identity},
		Verify:       verify,
		WriteTimeout: 10 * time.Second,
	}, nil
}

func verifyServerCode(identity *ProcessIdentity, expected string) error {
	if identity == nil || identity.CodeID == "" || subtle.ConstantTimeCompare([]byte(identity.CodeID), []byte(expected)) != 1 {
		return ErrBrokerIdentity
	}
	if err := identity.Verify(); err != nil {
		return err
	}
	return nil
}

type executableServerConn struct {
	*tls.Conn
	raw      net.Conn
	identity *ProcessIdentity
	close    sync.Once
	closeErr error
}

func (c *executableServerConn) Close() error {
	c.close.Do(func() {
		c.closeErr = c.raw.Close()
		c.identity.Close()
	})
	return c.closeErr
}

var _ net.Conn = (*executableServerConn)(nil)
var _ interface{ ConnectionState() tls.ConnectionState } = (*executableServerConn)(nil)
