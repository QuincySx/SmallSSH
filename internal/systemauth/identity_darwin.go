//go:build darwin

package systemauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// IdentityAgentPath locates only the build-pinned native SSH signer. This is
// distinct from the device key used to unlock the encrypted connection vault.
func IdentityAgentPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	var expected map[string]string
	if json.Unmarshal(hashes, &expected) != nil {
		return "", errors.New("密钥代理校验记录无效")
	}
	want := expected["darwin/"+runtime.GOARCH+"/identity"]
	if len(want) != 64 {
		return "", errors.New("请先构建原生 SSH 密钥代理")
	}
	base := filepath.Dir(exe)
	for _, dir := range []string{base, filepath.Join(base, "..", "Resources"), filepath.Join(base, "..")} {
		path := filepath.Join(dir, "SmallSSHIdentityAgent.app", "Contents", "MacOS", "SmallSSHIdentityAgent")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) == want {
			return path, nil
		}
	}
	return "", errors.New("原生 SSH 密钥代理缺失或校验不匹配")
}
