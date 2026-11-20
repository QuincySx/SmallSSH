// Package sshdata contains transport-neutral SSH metadata shared over local IPC.
package sshdata

import (
	"errors"
	"time"
)

var ErrUploadTargetExists = errors.New("远程同名文件已存在")

type HostIdentity struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

type KeyInfo struct {
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment,omitempty"`
}

type File struct {
	Modified  time.Time `json:"modified"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Directory bool      `json:"directory"`
	Mode      string    `json:"mode"`
}

type TransferMeta struct {
	Size       int64  `json:"size"`
	Offset     int64  `json:"offset"`
	Hash       string `json:"hash"`
	PrefixHash string `json:"prefixHash"`
}

type RouteMeasurement struct {
	Relay           bool   `json:"relay"`
	Available       bool   `json:"available"`
	HandshakeMillis int64  `json:"handshakeMillis"`
	Error           string `json:"error,omitempty"`
}
