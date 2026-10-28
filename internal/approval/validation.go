package approval

import (
	"encoding/json"
	"errors"
	"regexp"

	"sshclient/internal/fileattrs"
	"sshclient/internal/serverops"
)

const MaxOperationBytes = 64 << 10

var contentDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var transferIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{24}$`)

// Reject irrelevant fields rather than storing attacker-controlled payloads in
// the queue. The encoded limit also bounds escaping and paginated responses.
func validateFields(op Operation) error {
	remaining := op
	remaining.Host, remaining.Kind, remaining.Client, remaining.Endpoint, remaining.HostFingerprint = "", "", "", "", ""
	switch op.Kind {
	case "exec":
		remaining.Command = ""
	case "shell":
		remaining.ServerStatus = false
	case "grant":
		remaining.Grant, remaining.PathProtection = GrantScope{}, false
	case "list", "get", "file-info", "remove":
		remaining.Path = ""
	case "mkdir", "chmod":
		remaining.Path, remaining.Mode = "", 0
	case "rename":
		remaining.Path, remaining.Destination = "", ""
	case "file-attributes":
		remaining.Path, remaining.Attributes = "", fileattrs.Change{}
	case "put":
		remaining.Path, remaining.ContentHash, remaining.TransferID, remaining.Size = "", "", "", 0
	case "replace":
		remaining.Path, remaining.ContentHash, remaining.OriginalHash = "", "", ""
	case "put-resume", "get-resume":
		remaining.Path, remaining.TransferID, remaining.ContentHash, remaining.PrefixHash = "", "", "", ""
		remaining.Size, remaining.Offset = 0, 0
	case "docker-logs", "docker-terminal":
		remaining.Docker, remaining.ServerStatus = serverops.DockerRequest{}, false
	case "compose-plan", "compose-update":
		remaining.Compose = serverops.ComposeRequest{}
	case "monitor":
	}
	if remaining != (Operation{}) {
		return errors.New("操作包含无关字段")
	}
	for _, value := range []string{op.ContentHash, op.OriginalHash, op.PrefixHash} {
		if value != "" && !contentDigest.MatchString(value) {
			return errors.New("内容指纹必须为小写 SHA256")
		}
	}
	if op.TransferID != "" && !transferIdentifier.MatchString(op.TransferID) {
		return errors.New("传输 ID 无效")
	}
	if op.Size < 0 || op.Offset < 0 {
		return errors.New("传输大小与偏移不能为负数")
	}
	if op.Kind == "put" && (op.TransferID == "" && op.Size != 0) {
		return errors.New("有界上传需指定传输 ID")
	}
	if op.Kind == "put-resume" && op.Offset != 0 {
		return errors.New("上传偏移由代理核对后确定")
	}
	b, err := json.Marshal(op)
	if err != nil || len(b) > MaxOperationBytes {
		return errors.New("操作内容超过上限")
	}
	return nil
}
