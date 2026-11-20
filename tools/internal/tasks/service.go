package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

// runService checks the real SDK identity handshake without opening a desktop
// window, unlocking a vault or granting any caller access.
func runService(args []string) error {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	dir := fs.String("data-dir", localipc.DefaultDir(), "私有数据目录")
	executable := fs.String("executable", "", "预期后台可执行文件的绝对路径")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*executable) {
		return errors.New("用法: go run ./tools service -executable /后台程序绝对路径 [-data-dir 私有目录]")
	}
	out, logPath, err := openLog("service", false)
	if err != nil {
		return err
	}
	defer out.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := (client.ServiceClient{Dir: *dir, Executable: *executable}).Status(ctx)
	if err != nil {
		fmt.Fprintf(out, "后台身份与状态检查失败：%v\n", err)
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Log    string                 `json:"log"`
		Status protocol.ServiceStatus `json:"status"`
	}{logPath, status})
}
