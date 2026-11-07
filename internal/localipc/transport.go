package localipc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"os"
	"time"
)

var ErrBrokerIdentity = errors.New("代理身份校验失败；请在可信的 SmallSSH 桌面检查或修复本机 CLI 配对，也可显式指定 --broker-key")

// Close aborts the local transport without waiting for a TLS close-notify write.
func (w *Wire) Close() error {
	if connection, ok := w.Conn.(*tls.Conn); ok {
		return connection.NetConn().Close()
	}
	return w.Conn.Close()
}

func ServerTLS(identity ...ed25519.PrivateKey) (*tls.Config, string, error) {
	var private ed25519.PrivateKey
	if len(identity) > 1 || len(identity) == 1 && len(identity[0]) != ed25519.PrivateKeySize {
		return nil, "", ErrBrokerIdentity
	}
	if len(identity) == 1 {
		private = append(ed25519.PrivateKey(nil), identity[0]...)
	} else {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, "", err
		}
		private = key
	}
	public := private.Public().(ed25519.PublicKey)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "SmallSSH local broker"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		return nil, "", err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, "", err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}}, publicKeyPin(certificate), nil
}

func publicKeyPin(certificate *x509.Certificate) string {
	digest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// DestroyServerTLS is called after every server connection and handshake has
// stopped. Clearing this owned slice does not claim whole-process erasure.
func DestroyServerTLS(configuration *tls.Config) {
	if configuration == nil {
		return
	}
	for _, certificate := range configuration.Certificates {
		if key, ok := certificate.PrivateKey.(ed25519.PrivateKey); ok {
			clear(key)
		}
	}
}

// ConnectVerified never learns a trust anchor from the endpoint it is checking.
// The sole pin-free case is a kernel-identified connection back into this exact
// process (the desktop hosts its broker in process). External callers use an
// owner-installed trust anchor or an explicitly provided public-key pin.
func ConnectVerified(ctx context.Context, dir, pin string) (*Wire, error) {
	raw, err := Dial(ctx, dir)
	if err != nil {
		return nil, err
	}
	self := false
	if pin == "" {
		pid, err := ServerPID(raw)
		self = err == nil && pid == os.Getpid()
		if !self {
			pin, err = ReadBrokerTrust(dir)
			if err != nil {
				raw.Close()
				return nil, err
			}
		}
	}
	if !self && !validBrokerPin(pin) {
		raw.Close()
		return nil, ErrBrokerIdentity
	}
	connection := tls.Client(raw, &tls.Config{
		MinVersion: tls.VersionTLS13,
		// There is no DNS identity or public CA for private local IPC. Trust is
		// the explicit SPKI pin (or the kernel-verified in-process endpoint),
		// enforced below for every TLS handshake, never a TOFU exception.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 {
				return ErrBrokerIdentity
			}
			certificate := state.PeerCertificates[0]
			if time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
				return ErrBrokerIdentity
			}
			if !self && subtle.ConstantTimeCompare([]byte(publicKeyPin(certificate)), []byte(pin)) != 1 {
				return ErrBrokerIdentity
			}
			return nil
		},
	})
	connection.SetDeadline(time.Now().Add(10 * time.Second))
	if err := connection.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	connection.SetDeadline(time.Time{})
	return &Wire{Conn: connection}, nil
}

func ChannelBinding(c net.Conn) (string, error) {
	connection, ok := c.(interface{ ConnectionState() tls.ConnectionState })
	if !ok {
		return "", ErrBrokerIdentity
	}
	state := connection.ConnectionState()
	bytes, err := state.ExportKeyingMaterial("smallssh/ipc-channel/v2", nil, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
