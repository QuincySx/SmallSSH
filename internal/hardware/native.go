package hardware

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/crypto/ssh"
	"sshclient/internal/connectcfg"
)

// Function indexes and ABI are defined by OASIS PKCS#11 2.40. No provider's
// private extension, default PIN, private-key export or token mutation is used.
type nativeModule struct {
	unload             func()
	finalize           func(unsafe.Pointer) word
	slots              func(uint8, unsafe.Pointer, *word) word
	token              func(word, unsafe.Pointer) word
	open               func(word, word, unsafe.Pointer, uintptr, *word) word
	close              func(word) word
	login              func(word, word, unsafe.Pointer, word) word
	attribute          func(word, word, unsafe.Pointer, word) word
	findInit           func(word, unsafe.Pointer, word) word
	find               func(word, unsafe.Pointer, word, *word) word
	findFinal          func(word) word
	signInit           func(word, unsafe.Pointer, word) word
	signValue          func(word, unsafe.Pointer, word, unsafe.Pointer, *word) word
	sessions           []word
	session, key       word
	public             crypto.PublicKey
	pin                []byte
	alwaysAuthenticate bool
}

func check(rv word, operation string) error {
	if rv != 0 {
		return fmt.Errorf("PKCS#11 %s 失败（0x%x）；请核对设备、PIN 和触摸要求", operation, rv)
	}
	return nil
}
func openNative(config connectcfg.Hardware) (*nativeModule, error) {
	hash, e := ModuleHash(config.Module)
	if e != nil {
		return nil, e
	}
	if hash != config.ModuleSHA256 {
		return nil, errors.New("PKCS#11 模块摘要已变化，请由所有者重新选择驱动")
	}
	symbol, unload, e := loadModule(config.Module)
	if e != nil {
		return nil, fmt.Errorf("无法加载 PKCS#11 原生模块：%w", e)
	}
	n := &nativeModule{unload: unload}
	var list unsafe.Pointer
	var getList func(*unsafe.Pointer) word
	purego.RegisterFunc(&getList, symbol)
	if e = check(getList(&list), "GetFunctionList"); e != nil {
		unload()
		return nil, e
	}
	if list == nil {
		unload()
		return nil, errors.New("PKCS#11 函数表无效")
	}
	if major := *(*byte)(list); major != 2 && major != 3 {
		unload()
		return nil, errors.New("PKCS#11 模块需提供 2.x 或 3.x 函数表")
	}
	bind := func(out any, index int) bool {
		address := *(*uintptr)(unsafe.Add(list, functionOffset+index*int(unsafe.Sizeof(uintptr(0)))))
		if address == 0 {
			return false
		}
		purego.RegisterFunc(out, address)
		return true
	}
	var initialize func(unsafe.Pointer) word
	ok := bind(&initialize, 0) && bind(&n.finalize, 1) && bind(&n.slots, 4) && bind(&n.token, 6) && bind(&n.open, 12) && bind(&n.close, 13) && bind(&n.login, 18) && bind(&n.attribute, 24) && bind(&n.findInit, 26) && bind(&n.find, 27) && bind(&n.findFinal, 28) && bind(&n.signInit, 42) && bind(&n.signValue, 43)
	if !ok {
		unload()
		return nil, errors.New("PKCS#11 模块缺少必需函数")
	}
	if e = check(initialize(nil), "Initialize"); e != nil {
		unload()
		return nil, e
	}
	return n, nil
}
func (n *nativeModule) Close() {
	clear(n.pin)
	for _, s := range n.sessions {
		n.close(s)
	}
	n.finalize(nil)
	n.unload()
}
func putWord(b []byte, v word) {
	if unsafe.Sizeof(v) == 4 {
		binary.LittleEndian.PutUint32(b, uint32(v))
	} else {
		binary.LittleEndian.PutUint64(b, uint64(v))
	}
}
func getWord(b []byte) word {
	if unsafe.Sizeof(word(0)) == 4 {
		return word(binary.LittleEndian.Uint32(b))
	}
	return word(binary.LittleEndian.Uint64(b))
}
func attr(kind word, value []byte) []byte {
	b := make([]byte, attributeSize)
	putWord(b, kind)
	if len(value) > 0 {
		binary.LittleEndian.PutUint64(b[valueOffset:], uint64(uintptr(unsafe.Pointer(&value[0]))))
	}
	putWord(b[lengthOffset:], word(len(value)))
	return b
}
func wordBytes(value word) []byte {
	b := make([]byte, unsafe.Sizeof(value))
	putWord(b, value)
	return b
}
func (n *nativeModule) get(session, object, kind word) ([]byte, error) {
	a := attr(kind, nil)
	if e := check(n.attribute(session, object, unsafe.Pointer(&a[0]), 1), "GetAttributeValue"); e != nil {
		return nil, e
	}
	length := getWord(a[lengthOffset:])
	if length == 0 || length > 32<<10 {
		return nil, errors.New("硬件公开属性为空或超过上限")
	}
	value := make([]byte, int(length))
	a = attr(kind, value)
	rv := n.attribute(session, object, unsafe.Pointer(&a[0]), 1)
	runtime.KeepAlive(value)
	if e := check(rv, "GetAttributeValue"); e != nil {
		return nil, e
	}
	length = getWord(a[lengthOffset:])
	if int(length) > len(value) {
		return nil, errors.New("硬件公开属性在读取期间变化")
	}
	return value[:length], nil
}
func (n *nativeModule) objects(session, class word, id []byte) ([]word, error) {
	classBytes := wordBytes(class)
	a := attr(0, classBytes)
	if len(id) > 0 {
		a = append(a, attr(0x102, id)...)
	}
	rv := n.findInit(session, unsafe.Pointer(&a[0]), word(len(a)/attributeSize))
	if e := check(rv, "FindObjectsInit"); e != nil {
		return nil, e
	}
	defer func() {
		n.findFinal(session)
		runtime.KeepAlive(classBytes)
		runtime.KeepAlive(id)
		runtime.KeepAlive(a)
	}()
	var objects []word
	for {
		var batch [16]word
		var count word
		if e := check(n.find(session, unsafe.Pointer(&batch[0]), 16, &count), "FindObjects"); e != nil {
			return nil, e
		}
		if count > 16 {
			return nil, errors.New("硬件对象计数无效")
		}
		objects = append(objects, batch[:count]...)
		if len(objects) > 128 {
			return nil, errors.New("硬件对象超过 128 个")
		}
		if count == 0 {
			return objects, nil
		}
	}
}
func (n *nativeModule) publicKey(session, object, class word) (crypto.PublicKey, error) {
	if class == 1 {
		b, e := n.get(session, object, 0x11)
		if e != nil {
			return nil, e
		}
		cert, e := x509.ParseCertificate(b)
		if e != nil {
			return nil, e
		}
		return cert.PublicKey, nil
	}
	kind, e := n.get(session, object, 0x100)
	if e != nil {
		return nil, e
	}
	if len(kind) != int(unsafe.Sizeof(word(0))) {
		return nil, errors.New("PKCS#11 密钥类型属性长度无效")
	}
	switch getWord(kind) {
	case 0:
		modulus, e := n.get(session, object, 0x120)
		if e != nil {
			return nil, e
		}
		exponent, e := n.get(session, object, 0x122)
		if e != nil {
			return nil, e
		}
		exp := new(big.Int).SetBytes(exponent)
		if !exp.IsInt64() || exp.Int64() < 3 || exp.Int64() > 1<<31 {
			return nil, errors.New("硬件 RSA 指数无效")
		}
		key := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: int(exp.Int64())}
		if key.N.BitLen() < 2048 || key.N.BitLen() > 8192 {
			return nil, errors.New("硬件 RSA 密钥需为 2048–8192 位")
		}
		return key, nil
	case 3:
		params, e := n.get(session, object, 0x180)
		if e != nil {
			return nil, e
		}
		point, e := n.get(session, object, 0x181)
		if e != nil {
			return nil, e
		}
		var oid asn1.ObjectIdentifier
		if _, e = asn1.Unmarshal(params, &oid); e != nil {
			return nil, e
		}
		var curve elliptic.Curve
		switch oid.String() {
		case "1.2.840.10045.3.1.7":
			curve = elliptic.P256()
		case "1.3.132.0.34":
			curve = elliptic.P384()
		case "1.3.132.0.35":
			curve = elliptic.P521()
		default:
			return nil, errors.New("硬件 EC 曲线不受支持")
		}
		raw := point
		// Some providers return the unwrapped X9.62 point. Distinguish it
		// by its exact curve-dependent length before parsing the OCTET STRING:
		// its first coordinate byte can itself look like a valid DER length.
		if len(point) != 1+2*((curve.Params().BitSize+7)/8) || point[0] != 4 {
			if rest, e := asn1.Unmarshal(point, &raw); e != nil || len(rest) != 0 {
				return nil, errors.New("硬件 EC 公钥编码无效")
			}
		}
		x, y := elliptic.Unmarshal(curve, raw)
		if x == nil {
			return nil, errors.New("硬件 EC 公钥无效")
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
	}
	return nil, errors.New("硬件密钥仅支持 RSA 和 NIST ECDSA；FIDO2 请使用 SSH agent")
}
func (n *nativeModule) keys(config connectcfg.Hardware, selectKey bool) ([]Key, error) {
	if config.PIN != "" && config.TokenSerial == "" {
		return nil, errors.New("请先选择令牌序列号，再输入 PIN；不会向多个令牌尝试同一 PIN")
	}
	var slots []word
	for attempt := 0; attempt < 3; attempt++ {
		var count word
		if e := check(n.slots(1, nil, &count), "GetSlotList"); e != nil {
			return nil, e
		}
		if count == 0 || count > 64 {
			return nil, errors.New("未检测到令牌，或令牌数量超过 64")
		}
		slots = make([]word, count)
		rv := n.slots(1, unsafe.Pointer(&slots[0]), &count)
		if rv == 0x150 { // CKR_BUFFER_TOO_SMALL: slots changed between calls.
			slots = nil
			continue
		}
		if e := check(rv, "GetSlotList"); e != nil {
			return nil, e
		}
		if count > word(len(slots)) {
			return nil, errors.New("令牌数量在读取期间变化")
		}
		slots = slots[:count]
		break
	}
	if slots == nil {
		return nil, errors.New("令牌数量持续变化，请重新扫描")
	}
	type tokenSlot struct {
		slot word
		info [256]byte
	}
	var matching []tokenSlot
	for _, slot := range slots {
		entry := tokenSlot{slot: slot}
		rv := n.token(slot, unsafe.Pointer(&entry.info[0]))
		// A token may disappear after GetSlotList; other slots remain usable.
		if rv == 0xe0 || rv == 0x32 { // CKR_TOKEN_NOT_PRESENT, CKR_DEVICE_REMOVED
			continue
		}
		if e := check(rv, "GetTokenInfo"); e != nil {
			return nil, e
		}
		serial := strings.TrimSpace(string(entry.info[80:96]))
		if config.TokenSerial == "" || serial == config.TokenSerial {
			matching = append(matching, entry)
		}
	}
	// A serial identifies the token to which a PIN may be sent. Reject an
	// ambiguous selection before opening or authenticating either token.
	if config.TokenSerial != "" && len(matching) > 1 {
		return nil, errors.New("多个令牌具有相同序列号，无法安全选择令牌")
	}
	var keys []Key
	seen := map[string]bool{}
	for _, entry := range matching {
		slot, info := entry.slot, entry.info
		serial := strings.TrimSpace(string(info[80:96]))
		var session word
		if e := check(n.open(slot, 4, nil, 0, &session), "OpenSession"); e != nil {
			return nil, e
		}
		n.sessions = append(n.sessions, session)
		if config.PIN != "" || getWord(info[96:])&0x100 != 0 && config.TokenSerial != "" {
			pin := []byte(config.PIN)
			if getWord(info[96:])&0x100 != 0 {
				clear(pin)
				pin = nil
			}
			var ptr unsafe.Pointer
			if len(pin) > 0 {
				ptr = unsafe.Pointer(&pin[0])
			}
			rv := n.login(session, 1, ptr, word(len(pin)))
			clear(pin)
			if rv != 0 && rv != 0x100 {
				return nil, check(rv, "Login（不会自动重试 PIN）")
			}
		}
		token := Key{Token: strings.TrimSpace(string(info[:32])), Model: strings.TrimSpace(string(info[64:80])), Serial: serial}
		found := false
		for _, class := range []word{2, 1} {
			objects, e := n.objects(session, class, nil)
			if e != nil {
				return nil, e
			}
			for _, object := range objects {
				id, e := n.get(session, object, 0x102)
				if e != nil {
					continue
				}
				pub, e := n.publicKey(session, object, class)
				if e != nil {
					continue
				}
				switch public := pub.(type) {
				case *rsa.PublicKey:
					if public.N.BitLen() < 2048 || public.N.BitLen() > 8192 {
						continue
					}
				case *ecdsa.PublicKey:
				default:
					continue
				}
				sshPub, e := ssh.NewPublicKey(pub)
				if e != nil {
					continue
				}
				key := token
				key.ID = hex.EncodeToString(id)
				key.Fingerprint = ssh.FingerprintSHA256(sshPub)
				key.Algorithm = sshPub.Type()
				if seen[key.Serial+key.ID+key.Fingerprint] {
					continue
				}
				seen[key.Serial+key.ID+key.Fingerprint] = true
				label, _ := n.get(session, object, 3)
				key.Label = strings.TrimSpace(string(label))
				key.Public, _ = x509.MarshalPKIXPublicKey(pub)
				if selectKey {
					if key.ID != config.KeyID || key.Fingerprint != config.Fingerprint {
						continue
					}
					private, e := n.objects(session, 3, id)
					if e != nil {
						return nil, e
					}
					if len(private) != 1 {
						return nil, errors.New("硬件私钥不可用或密钥 ID 不唯一")
					}
					n.session, n.key, n.public = session, private[0], pub
					if getWord(info[96:])&0x100 == 0 {
						n.pin = append([]byte(nil), config.PIN...)
					}
					if attribute, e := n.get(session, private[0], 0x202); e == nil && len(attribute) == 1 {
						n.alwaysAuthenticate = attribute[0] != 0
					}
				}
				keys = append(keys, key)
				found = true
				if len(keys) > 128 {
					return nil, errors.New("硬件公钥超过 128 个")
				}
			}
		}
		if !found && !selectKey {
			keys = append(keys, token)
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("未找到选定的令牌或公钥；请检查插入、PIN 和密钥 ID")
	}
	if selectKey && len(keys) != 1 {
		return nil, errors.New("硬件公钥选择不唯一")
	}
	return keys, nil
}
func (n *nativeModule) sign(digest []byte, hash crypto.Hash) ([]byte, error) {
	if n.public == nil {
		return nil, errors.New("硬件密钥尚未选定")
	}
	if hash != crypto.SHA256 && hash != crypto.SHA384 && hash != crypto.SHA512 || len(digest) != hash.Size() {
		return nil, errors.New("硬件签名摘要算法或长度无效")
	}
	mechanism := word(0x1041)
	input := digest
	if _, ok := n.public.(*rsa.PublicKey); ok {
		mechanism = 1
		prefixes := map[crypto.Hash]string{crypto.SHA256: "3031300d060960864801650304020105000420", crypto.SHA384: "3041300d060960864801650304020205000430", crypto.SHA512: "3051300d060960864801650304020305000440"}
		prefix, _ := hex.DecodeString(prefixes[hash])
		input = append(prefix, digest...)
	}
	parameters := make([]byte, mechanismSize)
	putWord(parameters, mechanism)
	if e := check(n.signInit(n.session, unsafe.Pointer(&parameters[0]), n.key), "SignInit"); e != nil {
		return nil, e
	}
	if n.alwaysAuthenticate {
		var pointer unsafe.Pointer
		if len(n.pin) > 0 {
			pointer = unsafe.Pointer(&n.pin[0])
		}
		if e := check(n.login(n.session, 2, pointer, word(len(n.pin))), "Login（本次签名确认，不自动重试）"); e != nil {
			return nil, e
		}
	}
	signature := make([]byte, 8192)
	size := word(len(signature))
	rv := n.signValue(n.session, unsafe.Pointer(&input[0]), word(len(input)), unsafe.Pointer(&signature[0]), &size)
	runtime.KeepAlive(input)
	if e := check(rv, "Sign"); e != nil {
		return nil, e
	}
	if size == 0 || int(size) > len(signature) {
		return nil, errors.New("硬件签名长度无效")
	}
	signature = signature[:size]
	if pub, ok := n.public.(*ecdsa.PublicKey); ok {
		width := (pub.Curve.Params().BitSize + 7) / 8
		if len(signature) != 2*width {
			return nil, errors.New("硬件 ECDSA 签名长度无效")
		}
		r, s := new(big.Int).SetBytes(signature[:width]), new(big.Int).SetBytes(signature[width:])
		if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(pub.Curve.Params().N) >= 0 || s.Cmp(pub.Curve.Params().N) >= 0 {
			return nil, errors.New("硬件 ECDSA 签名无效")
		}
		return asn1.Marshal(struct{ R, S *big.Int }{r, s})
	}
	return signature, nil
}
