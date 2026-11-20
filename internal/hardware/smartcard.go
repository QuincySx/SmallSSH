package hardware

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"errors"

	"golang.org/x/crypto/ssh"
)

// Security.framework exports public RSA keys as PKCS#1 and EC keys as
// uncompressed ANSI X9.63 points. Present the same metadata as PKCS#11.
func normalizeMacSmartCardKeys(keys []Key) ([]Key, error) {
	for i := range keys {
		key := &keys[i]
		var public crypto.PublicKey
		switch key.Algorithm {
		case "rsa":
			parsed, err := x509.ParsePKCS1PublicKey(key.Public)
			if err != nil || parsed.N.BitLen() < 2048 || parsed.N.BitLen() > 8192 {
				return nil, errors.New("系统智能卡的 RSA 公钥无效")
			}
			public = parsed
		case "ec":
			curve := elliptic.P256()
			switch len(key.Public) {
			case 97:
				curve = elliptic.P384()
			case 133:
				curve = elliptic.P521()
			}
			x, y := elliptic.Unmarshal(curve, key.Public)
			if x == nil {
				return nil, errors.New("系统智能卡的 EC 公钥无效")
			}
			public = &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
		default:
			return nil, errors.New("系统智能卡公钥类型不受支持")
		}
		sshKey, err := ssh.NewPublicKey(public)
		if err != nil {
			return nil, err
		}
		key.Public, err = x509.MarshalPKIXPublicKey(public)
		if err != nil {
			return nil, err
		}
		key.Algorithm, key.Fingerprint = sshKey.Type(), ssh.FingerprintSHA256(sshKey)
	}
	return keys, nil
}
