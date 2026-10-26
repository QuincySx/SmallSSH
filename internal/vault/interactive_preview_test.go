//go:build preview_qa

package vault_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"golang.org/x/crypto/argon2"
	"os"
	"path/filepath"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"testing"
	"time"
)

// This opt-in test creates a disposable loopback fixture. The short, user-
// supplied QA password does not change the production creation policy.
func TestInteractivePreviewFixture(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("manual UI QA only")
	}
	dir := os.Getenv("SSH_CLIENT_QA_DIR")
	if filepath.Base(dir) != "ssh-client-workbench-preview-qa" {
		t.Fatal("explicit QA directory required")
	}
	if e := localipc.Prepare(dir); e != nil {
		t.Fatal(e)
	}
	fixture := testserver.New(t)
	header := vault.Header{Version: vault.Version, Salt: make([]byte, 16), ID: make([]byte, 16)}
	rand.Read(header.Salt)
	rand.Read(header.ID)
	key := argon2.IDKey([]byte("123123"), header.Salt, 3, 64*1024, 1, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	profile := fixture.Profile()
	profile.Group = "开发测试"
	profile.Name = "本机测试 SSH"
	plain, _ := json.Marshal(map[string]any{"profiles": []vault.Profile{profile}})
	aad, _ := json.Marshal(header)
	data, _ := json.Marshal(struct {
		Header     vault.Header `json:"header"`
		Nonce      []byte       `json:"nonce"`
		Ciphertext []byte       `json:"ciphertext"`
	}{header, nonce, gcm.Seal(nil, nonce, plain, aad)})
	f, e := os.OpenFile(filepath.Join(dir, "vault.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		t.Fatal(e)
	}
	f.Close()
	testDir := t.TempDir()
	keyPath := filepath.Join(testDir, "loopback-test-key.pem")
	if e = os.WriteFile(keyPath, []byte(fixture.PrivateKey), 0600); e != nil {
		t.Fatal(e)
	}
	// Explicit opt-in fixture inputs only; never copied from user credentials.
	inputs, _ := json.Marshal(map[string]any{"host": profile.Host, "port": profile.Port, "user": profile.User, "fingerprint": profile.Fingerprint, "password": profile.Password, "privateKeyPath": keyPath})
	if e = os.WriteFile(filepath.Join(dir, "qa-connection.json"), inputs, 0600); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(testDir, "example.txt"), []byte("original QA text\n"), 0600)
	t.Log("Loopback fixture ready. File directory:", testserver.RemotePath(testDir))
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-timer.C:
			return
		case <-tick.C:
			if _, e := os.Stat(filepath.Join(dir, "qa-stop")); e == nil {
				return
			}
		}
	}
}
