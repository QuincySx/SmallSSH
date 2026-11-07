package localipc

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrBrokerPairing = errors.New("尚未配对本机代理：请先在可信的 SmallSSH 桌面解锁保险库；首次解锁会建立代理身份信任，之后无需复制指纹")

type brokerTrust struct {
	Version int    `json:"version"`
	Pin     string `json:"brokerKey"`
}

func validBrokerPin(pin string) bool {
	if len(pin) != 71 || !strings.HasPrefix(pin, "sha256:") {
		return false
	}
	key, err := hex.DecodeString(pin[7:])
	return err == nil && len(key) == 32
}

// ReadBrokerTrust reads an owner-installed public trust anchor, never a key
// learned from the endpoint being authenticated. Integrity of this owner
// configuration, like the executable itself, relies on the OS account boundary.
func ReadBrokerTrust(dir string) (string, error) {
	name := filepath.Join(dir, "broker-trust.json")
	before, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return "", ErrBrokerPairing
	}
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() || before.Size() > 1024 || runtime.GOOS != "windows" && before.Mode().Perm()&0077 != 0 {
		return "", errors.New("代理配对记录的类型、大小或权限无效")
	}
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", errors.New("代理配对记录在读取期间变化")
	}
	var trust brokerTrust
	decoder := json.NewDecoder(io.LimitReader(f, 1025))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&trust) != nil || trust.Version != 1 || !validBrokerPin(trust.Pin) {
		return "", errors.New("代理配对记录无效，请在可信桌面重新配对")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "", errors.New("代理配对记录包含多余内容")
	}
	return trust.Pin, nil
}

// PairBroker is called only by an authenticated owner. Initial creation never
// replaces another identity; replacement requires the owner's explicit action.
func PairBroker(dir, pin string, replace bool) error {
	if !validBrokerPin(pin) {
		return ErrBrokerIdentity
	}
	if err := Prepare(dir); err != nil {
		return err
	}
	name := filepath.Join(dir, "broker-trust.json")
	if existing, err := ReadBrokerTrust(dir); err == nil {
		if existing == pin {
			return nil
		}
		if !replace {
			return errors.New("代理身份与已有配对不同，请在可信桌面明确重新配对")
		}
	} else if !errors.Is(err, ErrBrokerPairing) && !replace {
		return err
	}
	if st, err := os.Lstat(name); err == nil && !st.Mode().IsRegular() {
		return errors.New("不替换非普通文件的配对记录")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, _ := json.Marshal(brokerTrust{Version: 1, Pin: pin})
	f, err := os.CreateTemp(dir, ".broker-trust-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if replace {
		return os.Rename(temp, name)
	}
	// Linking a complete private file publishes it without a partial-read or
	// check/replace race. An existing anchor can never be silently replaced.
	if err := os.Link(temp, name); err != nil {
		if current, readErr := ReadBrokerTrust(dir); readErr == nil && current == pin {
			return nil
		}
		return err
	}
	return nil
}
