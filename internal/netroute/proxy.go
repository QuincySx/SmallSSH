// Package netroute implements explicit proxy routes without environment fallback.
package netroute

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"sshclient/internal/connectcfg"
)

func Dial(ctx context.Context, target string, p connectcfg.Proxy) (net.Conn, error) {
	if e := connectcfg.Address(target, false); e != nil {
		return nil, e
	}
	if e := p.Validate(); e != nil {
		return nil, e
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	if p.Mode == "" || p.Mode == "none" {
		return dialer.DialContext(ctx, "tcp", target)
	}
	if p.Mode == "socks5" {
		return dialSOCKS(ctx, target, p, dialer)
	}
	raw, e := dialer.DialContext(ctx, "tcp", p.Address)
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			raw.Close()
		}
	}()
	socket := raw
	stop := context.AfterFunc(ctx, func() { socket.Close() })
	defer stop()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	raw.SetDeadline(deadline)
	if p.Mode == "https" {
		host, _, _ := net.SplitHostPort(p.Address)
		secure := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
		if e = secure.HandshakeContext(ctx); e != nil {
			return nil, errors.New("HTTPS 代理证书或握手验证失败")
		}
		raw = secure
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if p.User != "" {
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.User+":"+p.Password)))
	}
	if e = request.Write(raw); e != nil {
		return nil, e
	}
	reader := bufio.NewReaderSize(raw, 4096)
	// ReadResponse's header budget is enforced by the connection deadline and a
	// limited header reader; a proxy cannot allocate an unbounded header here.
	response, e := readResponse(reader, request)
	if e != nil {
		return nil, errors.New("HTTP 代理 CONNECT 响应无效")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP 代理拒绝 CONNECT（状态 %d）", response.StatusCode)
	}
	raw.SetDeadline(time.Time{})
	success = true
	return &bufferedConn{Conn: raw, reader: reader}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func readResponse(reader *bufio.Reader, request *http.Request) (*http.Response, error) {
	var header []byte
	for {
		line, e := reader.ReadSlice('\n')
		if len(header)+len(line) > 16<<10 {
			return nil, errors.New("proxy headers too large")
		}
		header = append(header, line...)
		if e != nil {
			return nil, e
		}
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			break
		}
	}
	return http.ReadResponse(bufio.NewReader(bytes.NewReader(header)), request)
}
