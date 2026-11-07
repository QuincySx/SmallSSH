package netroute

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"time"

	"sshclient/internal/connectcfg"
)

func dialSOCKS(ctx context.Context, target string, p connectcfg.Proxy, dialer *net.Dialer) (net.Conn, error) {
	c, e := dialer.DialContext(ctx, "tcp", p.Address)
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			c.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	method := byte(0)
	if p.User != "" {
		method = 2
	}
	if e = writeFull(c, []byte{5, 1, method}); e != nil {
		return nil, e
	}
	var answer [2]byte
	if _, e = io.ReadFull(c, answer[:]); e != nil {
		return nil, e
	}
	if answer != [2]byte{5, method} {
		return nil, errors.New("SOCKS5 代理不接受配置的认证方式")
	}
	if method == 2 {
		login := []byte{1, byte(len(p.User))}
		login = append(login, p.User...)
		login = append(login, byte(len(p.Password)))
		login = append(login, p.Password...)
		e = writeFull(c, login)
		clear(login)
		if e != nil {
			return nil, e
		}
		if _, e = io.ReadFull(c, answer[:]); e != nil {
			return nil, e
		}
		if answer != [2]byte{1, 0} {
			return nil, errors.New("SOCKS5 代理用户名或密码不正确")
		}
	}
	address, e := EncodeAddress(target)
	if e != nil {
		return nil, e
	}
	if e = writeFull(c, append([]byte{5, 1, 0}, address...)); e != nil {
		return nil, e
	}
	var header [3]byte
	if _, e = io.ReadFull(c, header[:]); e != nil {
		return nil, e
	}
	if header != [3]byte{5, 0, 0} {
		return nil, errors.New("SOCKS5 代理拒绝连接目标")
	}
	if _, e = ReadAddress(c); e != nil {
		return nil, e
	}
	c.SetDeadline(time.Time{})
	success = true
	return c, nil
}
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

// EncodeAddress and ReadAddress implement the bounded RFC 1928 TCP address.
// Names stay names: resolving them is the proxy/SSH server's responsibility.
func EncodeAddress(address string) ([]byte, error) {
	if e := connectcfg.Address(address, false); e != nil {
		return nil, e
	}
	host, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	var b []byte
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			b = append([]byte{1}, v4...)
		} else {
			b = append([]byte{4}, ip.To16()...)
		}
	} else {
		b = append([]byte{3, byte(len(host))}, host...)
	}
	return append(b, byte(n>>8), byte(n)), nil
}
func ReadAddress(reader io.Reader) (string, error) {
	var kind [1]byte
	if _, e := io.ReadFull(reader, kind[:]); e != nil {
		return "", e
	}
	length := 0
	isIP := kind[0] == 1 || kind[0] == 4
	switch kind[0] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		if _, e := io.ReadFull(reader, kind[:]); e != nil {
			return "", e
		}
		length = int(kind[0])
		if length == 0 {
			return "", errors.New("SOCKS5 域名为空")
		}
	default:
		return "", errors.New("SOCKS5 地址类型不支持")
	}
	data := make([]byte, length+2)
	if _, e := io.ReadFull(reader, data); e != nil {
		return "", e
	}
	host := string(data[:length])
	if isIP {
		host = net.IP(data[:length]).String()
	}
	address := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(data[length:]))))
	if e := connectcfg.Address(address, true); e != nil {
		return "", e
	}
	return address, nil
}
