package hardware

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"sshclient/internal/connectcfg"
)

// The worker owns the native references. The desktop and broker see only the
// same public identity and signature messages as with a PKCS#11 driver.
type macSmartCard struct {
	pool, context, selected objc.ID
	selectedAlgorithm       string
	query                   func(objc.ID, *objc.ID) int32
	attributes              func(objc.ID) objc.ID
	publicKey               func(objc.ID) objc.ID
	external                func(objc.ID, *objc.ID) objc.ID
	supported               func(objc.ID, int64, objc.ID) uint8
	createSignature         func(objc.ID, objc.ID, objc.ID, *objc.ID) objc.ID
	retain                  func(objc.ID) objc.ID
	release                 func(objc.ID)
	value                   func(objc.ID, objc.ID) objc.ID
	data                    func(uintptr, *byte, int64) objc.ID
	length                  func(objc.ID) int64
	bytePointer             func(objc.ID) *byte
	cstring                 func(objc.ID, *byte, int64, uint32) uint8
	errorCode               func(objc.ID) int64
	classKey, keyClass      objc.ID
	privateKey, keyType     objc.ID
	tokenID, label          objc.ID
	rsaType, ecType         objc.ID
	itemClass, matchLimit   objc.ID
	matchAll, returnRef     objc.ID
	authContext, cfTrue     objc.ID
	rsaAlgorithms           [3]objc.ID
	ecAlgorithms            [3]objc.ID
}

func openMacSmartCard() (hardwareSession, error) {
	const frameworks = "/System/Library/Frameworks/"
	cf, err := purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	security, err := purego.Dlopen(frameworks+"Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	for _, framework := range []string{"CryptoTokenKit", "LocalAuthentication"} {
		if _, err := purego.Dlopen(frameworks+framework+".framework/"+framework, purego.RTLD_NOW|purego.RTLD_LOCAL); err != nil {
			return nil, err
		}
	}
	m := &macSmartCard{}
	for _, binding := range []struct {
		lib  uintptr
		name string
		out  any
	}{
		{security, "SecItemCopyMatching", &m.query},
		{security, "SecKeyCopyAttributes", &m.attributes},
		{security, "SecKeyCopyPublicKey", &m.publicKey},
		{security, "SecKeyCopyExternalRepresentation", &m.external},
		{security, "SecKeyIsAlgorithmSupported", &m.supported},
		{security, "SecKeyCreateSignature", &m.createSignature},
		{cf, "CFRetain", &m.retain},
		{cf, "CFRelease", &m.release},
		{cf, "CFDictionaryGetValue", &m.value},
		{cf, "CFDataCreate", &m.data},
		{cf, "CFDataGetLength", &m.length},
		{cf, "CFDataGetBytePtr", &m.bytePointer},
		{cf, "CFStringGetCString", &m.cstring},
		{cf, "CFErrorGetCode", &m.errorCode},
	} {
		symbol, err := purego.Dlsym(binding.lib, binding.name)
		if err != nil {
			return nil, err
		}
		purego.RegisterFunc(binding.out, symbol)
	}
	var symbolPointer func(uintptr, string) unsafe.Pointer
	purego.RegisterLibFunc(&symbolPointer, cf, "dlsym")
	for _, binding := range []struct {
		lib  uintptr
		name string
		out  *objc.ID
	}{
		{security, "kSecClass", &m.itemClass},
		{security, "kSecClassKey", &m.classKey},
		{security, "kSecAttrKeyClass", &m.keyClass},
		{security, "kSecAttrKeyClassPrivate", &m.privateKey},
		{security, "kSecAttrKeyType", &m.keyType},
		{security, "kSecAttrKeyTypeRSA", &m.rsaType},
		{security, "kSecAttrKeyTypeECSECPrimeRandom", &m.ecType},
		{security, "kSecAttrTokenID", &m.tokenID},
		{security, "kSecAttrLabel", &m.label},
		{security, "kSecMatchLimit", &m.matchLimit},
		{security, "kSecMatchLimitAll", &m.matchAll},
		{security, "kSecReturnRef", &m.returnRef},
		{security, "kSecUseAuthenticationContext", &m.authContext},
		{cf, "kCFBooleanTrue", &m.cfTrue},
		{security, "kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA256", &m.rsaAlgorithms[0]},
		{security, "kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA384", &m.rsaAlgorithms[1]},
		{security, "kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA512", &m.rsaAlgorithms[2]},
		{security, "kSecKeyAlgorithmECDSASignatureDigestX962SHA256", &m.ecAlgorithms[0]},
		{security, "kSecKeyAlgorithmECDSASignatureDigestX962SHA384", &m.ecAlgorithms[1]},
		{security, "kSecKeyAlgorithmECDSASignatureDigestX962SHA512", &m.ecAlgorithms[2]},
	} {
		symbol := symbolPointer(binding.lib, binding.name)
		if symbol == nil || *(*objc.ID)(symbol) == 0 {
			return nil, fmt.Errorf("macOS 智能卡接口缺少 %s", binding.name)
		}
		*binding.out = *(*objc.ID)(symbol)
	}
	m.pool = objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	m.context = objc.ID(objc.GetClass("LAContext")).Send(objc.RegisterName("new"))
	if m.pool == 0 || m.context == 0 {
		m.Close()
		return nil, errors.New("无法创建系统智能卡会话")
	}
	return m, nil
}

func (m *macSmartCard) Close() {
	if m.selected != 0 {
		m.release(m.selected)
	}
	if m.context != 0 {
		m.context.Send(objc.RegisterName("invalidate"))
		m.context.Send(objc.RegisterName("release"))
	}
	if m.pool != 0 {
		m.pool.Send(objc.RegisterName("drain"))
	}
}

func macString(m *macSmartCard, value objc.ID) string {
	var buffer [4097]byte
	if value == 0 || m.cstring(value, &buffer[0], int64(len(buffer)), 0x08000100) == 0 {
		return ""
	}
	result, _, _ := bytes.Cut(buffer[:], []byte{0})
	return string(result)
}

func (m *macSmartCard) dataBytes(value objc.ID) []byte {
	if value == 0 {
		return nil
	}
	n, ptr := m.length(value), m.bytePointer(value)
	if n <= 0 || n > 8192 || ptr == nil {
		return nil
	}
	return bytes.Clone(unsafe.Slice(ptr, int(n)))
}

func (m *macSmartCard) keys(config connectcfg.Hardware, selectKey bool) ([]Key, error) {
	if config.PIN != "" {
		return nil, errors.New("系统智能卡的 PIN 由 macOS 验证")
	}
	m.context.Send(objc.RegisterName("setInteractionNotAllowed:"), !selectKey)
	watcher := objc.ID(objc.GetClass("TKTokenWatcher")).Send(objc.RegisterName("new"))
	if watcher == 0 {
		return nil, errors.New("无法读取 macOS 智能卡")
	}
	defer watcher.Send(objc.RegisterName("release"))
	tokens := watcher.Send(objc.RegisterName("tokenIDs"))
	count, atIndex := objc.RegisterName("count"), objc.RegisterName("objectAtIndex:")
	out := make([]Key, 0)
	for index := uintptr(0); index < uintptr(tokens.Send(count)); index++ {
		token := tokens.Send(atIndex, index)
		tokenName := macString(m, token)
		if !strings.HasPrefix(tokenName, "com.apple.pivtoken:") && !strings.HasPrefix(tokenName, "com.apple.CryptoTokenKit.pivtoken:") {
			continue
		}
		sum := sha256.Sum256([]byte(tokenName))
		serial := hex.EncodeToString(sum[:16])
		if config.TokenSerial != "" && config.TokenSerial != serial {
			continue
		}
		keys, err := m.tokenKeys(token, serial, config, selectKey)
		if err != nil {
			return nil, err
		}
		out = append(out, keys...)
	}
	if selectKey && len(out) != 1 {
		return nil, errors.New("所选智能卡或公钥不可用")
	}
	return out, nil
}

func (m *macSmartCard) tokenKeys(token objc.ID, serial string, config connectcfg.Hardware, selectKey bool) ([]Key, error) {
	dict := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("new"))
	if dict == 0 {
		return nil, errors.New("无法创建系统智能卡查询")
	}
	defer dict.Send(objc.RegisterName("release"))
	set := objc.RegisterName("setObject:forKey:")
	for _, pair := range [][2]objc.ID{
		{m.itemClass, m.classKey}, {m.keyClass, m.privateKey}, {m.tokenID, token},
		{m.matchLimit, m.matchAll}, {m.returnRef, m.cfTrue}, {m.authContext, m.context},
	} {
		dict.Send(set, pair[1], pair[0])
	}
	var result objc.ID
	switch status := m.query(dict, &result); status {
	case -25300: // errSecItemNotFound
		return nil, nil
	case 0:
		if result == 0 {
			return nil, errors.New("macOS 智能卡查询没有返回公钥")
		}
	default:
		return nil, fmt.Errorf("macOS 无法读取智能卡（%d）", status)
	}
	defer m.release(result)
	var keys []Key
	count, atIndex := objc.RegisterName("count"), objc.RegisterName("objectAtIndex:")
	for index := uintptr(0); index < uintptr(result.Send(count)); index++ {
		reference := result.Send(atIndex, index)
		key, ok := m.cardKey(reference, token, serial)
		if !ok {
			continue
		}
		if selectKey {
			if key.ID != config.KeyID || key.Fingerprint != config.Fingerprint {
				continue
			}
			if m.selected != 0 {
				return nil, errors.New("系统智能卡密钥选择不唯一")
			}
			m.selected, m.selectedAlgorithm = m.retain(reference), key.Algorithm
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (m *macSmartCard) cardKey(reference, token objc.ID, serial string) (Key, bool) {
	attrs := m.attributes(reference)
	if attrs == 0 {
		return Key{}, false
	}
	defer m.release(attrs)
	if macString(m, m.value(attrs, m.tokenID)) != macString(m, token) {
		return Key{}, false
	}
	var kind string
	switch macString(m, m.value(attrs, m.keyType)) {
	case macString(m, m.rsaType):
		kind = "rsa"
	case macString(m, m.ecType):
		kind = "ec"
	default:
		return Key{}, false
	}
	public := m.publicKey(reference)
	if public == 0 {
		return Key{}, false
	}
	defer m.release(public)
	external := m.external(public, nil)
	if external == 0 {
		return Key{}, false
	}
	defer m.release(external)
	raw := m.dataBytes(external)
	sum := sha256.Sum256(raw)
	keys, err := normalizeMacSmartCardKeys([]Key{{Token: "macOS PIV", Serial: serial, ID: hex.EncodeToString(sum[:]), Label: macString(m, m.value(attrs, m.label)), Algorithm: kind, Public: raw}})
	if err != nil {
		return Key{}, false
	}
	key := keys[0]
	hash := crypto.SHA256
	switch key.Algorithm {
	case "ecdsa-sha2-nistp384":
		hash = crypto.SHA384
	case "ecdsa-sha2-nistp521":
		hash = crypto.SHA512
	}
	return key, m.supported(reference, 0, m.algorithm(key.Algorithm, hash)) != 0
}

func (m *macSmartCard) algorithm(kind string, hash crypto.Hash) objc.ID {
	if hash != crypto.SHA256 && hash != crypto.SHA384 && hash != crypto.SHA512 {
		return 0
	}
	slot := int(hash - crypto.SHA256)
	if kind == "ssh-rsa" {
		return m.rsaAlgorithms[slot]
	}
	return m.ecAlgorithms[slot]
}

func (m *macSmartCard) sign(digest []byte, hash crypto.Hash) ([]byte, error) {
	if m.selected == 0 {
		return nil, errors.New("系统智能卡会话尚未选择密钥")
	}
	algorithm := m.algorithm(m.selectedAlgorithm, hash)
	if algorithm == 0 || hash.Size() != len(digest) || m.supported(m.selected, 0, algorithm) == 0 {
		return nil, errors.New("智能卡不支持所选 SHA-2 签名算法")
	}
	data := m.data(0, &digest[0], int64(len(digest)))
	runtime.KeepAlive(digest)
	if data == 0 {
		return nil, errors.New("无法读取智能卡签名摘要")
	}
	defer m.release(data)
	var nativeError objc.ID
	signature := m.createSignature(m.selected, algorithm, data, &nativeError)
	if nativeError != 0 {
		defer m.release(nativeError)
	}
	if signature == 0 {
		var code int64
		if nativeError != 0 {
			code = m.errorCode(nativeError)
		}
		return nil, fmt.Errorf("macOS 未完成智能卡签名（%d）", code)
	}
	defer m.release(signature)
	value := m.dataBytes(signature)
	if len(value) == 0 {
		return nil, errors.New("macOS 返回了无效的智能卡签名")
	}
	return value, nil
}
