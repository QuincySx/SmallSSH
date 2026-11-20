// Package sshkey validates local SSH keys and targets without a network runtime.
package sshkey

import (
	"errors"
	"golang.org/x/crypto/ssh"
	"sshclient/internal/sshdata"
	"strings"
)

func ValidateTarget(host string, port int) error {
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "\x00\r\n\t /\\") || port < 1 || port > 65535 {
		return errors.New("请填写有效的服务器地址和 1–65535 之间的端口")
	}
	return nil
}

func ParsePrivateKey(raw, passphrase string) (ssh.Signer, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(raw), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(raw))
	}
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, errors.New("这份私钥已加密，请填写私钥口令")
		}
		if _, _, _, _, pubErr := ssh.ParseAuthorizedKey([]byte(raw)); pubErr == nil {
			return nil, errors.New("选择的是公钥，请选择对应的私钥文件")
		}
		return nil, errors.New("私钥格式不受支持或私钥口令不正确")
	}
	return signer, nil
}

func InspectPrivateKey(raw, passphrase string) (sshdata.KeyInfo, error) {
	signer, err := ParsePrivateKey(raw, passphrase)
	if err != nil {
		return sshdata.KeyInfo{}, err
	}
	return sshdata.KeyInfo{Algorithm: signer.PublicKey().Type(), Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, nil
}
