package broker_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func newManagedKeyVault(t *testing.T, profiles ...vault.Profile) (string, *vault.Session, []byte, string) {
	t.Helper()
	dir := managedSessionsTempDir(t, "b04c3a-")
	source, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	for _, profile := range profiles {
		if err = source.Put(profile); err != nil {
			t.Fatal(err)
		}
	}
	if err = vault.Save(broker.VaultPath(dir), source, true); err != nil {
		t.Fatal(err)
	}
	key, err := source.UnlockKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clear(key) })
	pin, err := source.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	return dir, source, key, pin
}

func TestOpenManagedKeyReadsLatestVaultUnderLock(t *testing.T) {
	dir, source, key, pin := newManagedKeyVault(t)
	offline, err := vault.ReadKey(broker.VaultPath(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(offline.Close)
	added := testserver.New(t).Profile()
	if err = offline.SaveProfile(broker.VaultPath(dir), added); err != nil {
		t.Fatal(err)
	}
	offline.Close()
	if _, err = source.Profile(added.ID); err == nil {
		t.Fatal("stale source unexpectedly sees the offline profile")
	}

	core, err := broker.OpenManagedKey(dir, key, strings.Repeat("b", 32), pin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	loaded, err := core.Vault.Profile(added.ID)
	if err != nil || loaded.Name != added.Name || loaded.Password != added.Password {
		t.Fatalf("managed core did not read the latest vault: %#v, %v", loaded, err)
	}

	core.Close()
	reopened, err := vault.ReadKey(broker.VaultPath(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	persisted, err := reopened.Profile(added.ID)
	if err != nil || persisted.Name != added.Name || persisted.Password != added.Password {
		t.Fatalf("managed core close replaced the latest vault: %#v, %v", persisted, err)
	}
}

func TestOpenManagedKeyRejectsWrongKeyOrPinAndReleasesLock(t *testing.T) {
	t.Run("wrong key", func(t *testing.T) {
		dir, _, key, pin := newManagedKeyVault(t, testserver.New(t).Profile())
		wrongKey := append([]byte(nil), key...)
		wrongKey[0] ^= 0xff
		t.Cleanup(func() { clear(wrongKey) })

		core, err := broker.OpenManagedKey(dir, wrongKey, strings.Repeat("c", 32), pin)
		if err == nil || core != nil {
			if core != nil {
				core.Close()
			}
			t.Fatalf("wrong key returned core=%v, err=%v", core, err)
		}
		core, err = broker.OpenManagedKey(dir, key, strings.Repeat("c", 32), pin)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(core.Close)
	})

	t.Run("wrong pin", func(t *testing.T) {
		dir, _, key, pin := newManagedKeyVault(t, testserver.New(t).Profile())
		wrongPin := pin
		last := len(wrongPin) - 1
		if wrongPin[last] == '0' {
			wrongPin = wrongPin[:last] + "1"
		} else {
			wrongPin = wrongPin[:last] + "0"
		}

		core, err := broker.OpenManagedKey(dir, key, strings.Repeat("c", 32), wrongPin)
		if err == nil || core != nil {
			if core != nil {
				core.Close()
			}
			t.Fatalf("wrong pin returned core=%v, err=%v", core, err)
		}
		core, err = broker.OpenManagedKey(dir, key, strings.Repeat("c", 32), pin)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(core.Close)
	})
}

func TestOpenManagedKeyDoesNotReplaceRunningCore(t *testing.T) {
	dir, _, key, pin := newManagedKeyVault(t, testserver.New(t).Profile())
	originalKey := append([]byte(nil), key...)
	t.Cleanup(func() { clear(originalKey) })

	core, err := broker.OpenManagedKey(dir, key, strings.Repeat("d", 32), pin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	if !bytes.Equal(key, originalKey) {
		t.Fatal("successful constructor changed the caller key")
	}

	replacement, err := broker.OpenManagedKey(dir, key, strings.Repeat("e", 32), pin)
	if err == nil || replacement != nil {
		if replacement != nil {
			replacement.Close()
		}
		t.Fatalf("second constructor returned replacement=%v, err=%v", replacement, err)
	}
	if !bytes.Equal(key, originalKey) {
		t.Fatal("failed constructor changed the caller key")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pinned := client.Client{Dir: dir, BrokerKey: core.PublicKeyPin(), Owner: core.Vault}
	if _, err = pinned.ListSnippets(ctx, core.Vault); err != nil {
		t.Fatalf("original core stopped serving: %v", err)
	}

	clear(key)
	if _, err = pinned.ListSnippets(ctx, core.Vault); err != nil {
		t.Fatalf("original core depended on the caller key buffer: %v", err)
	}
}
