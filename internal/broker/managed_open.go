package broker

import (
	"errors"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/vault"
)

// OpenManagedKey starts a managed broker from a short-lived vault key obtained
// by the authenticated prepare flow. It takes the data-directory lock before
// reading the vault, so the session it hands to the core includes changes made
// while the host was waiting for UI approval. The prepared pin must still match
// the vault currently on disk.
func OpenManagedKey(dir string, key []byte, epoch, expectedPin string) (*Server, error) {
	if len(key) != 32 {
		return nil, errors.New("管理密钥长度必须为32字节")
	}
	if expectedPin == "" {
		return nil, errors.New("管理会话缺少已认证的代理公钥")
	}
	keyCopy := append([]byte(nil), key...)
	defer clear(keyCopy)

	release, err := localipc.Acquire(dir)
	if err != nil {
		return nil, err
	}
	v, err := vault.ReadKey(VaultPath(dir), keyCopy)
	if err != nil {
		release()
		return nil, err
	}
	pin, err := v.BrokerPublicKeyPin()
	if err != nil {
		v.Close()
		release()
		return nil, err
	}
	if pin != expectedPin {
		v.Close()
		release()
		return nil, errors.New("代理公钥与已认证的会话不匹配")
	}
	return openSessionWithRelease(dir, v, time.Now, epoch, true, release)
}
