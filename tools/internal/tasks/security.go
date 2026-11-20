package tasks

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sshclient/tools/internal/project"
)

func runSecurity(args []string) error {
	fs := flag.NewFlagSet("security", flag.ContinueOnError)
	packages := fs.String("pkg", "./cmd/desktop ./cmd/smallsshd ./cmd/ssh-client", "space-separated source entry packages")
	binary := fs.String("binary", "", "inspect a compiled Go executable instead of source")
	snapshot := fs.Bool("snapshot", false, "download the official database once with curl and analyze its local snapshot")
	if err := parse(fs, args); err != nil {
		return err
	}
	out, logPath, err := openLog("security", false)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := verifyVendorPatch(); err != nil {
		return err
	}
	jsonPath := strings.TrimSuffix(logPath, ".log") + ".json"
	result, err := os.Create(jsonPath)
	if err != nil {
		return err
	}
	defer result.Close()
	arguments := []string{"tool", "govulncheck", "-json"}
	if *snapshot {
		db, err := securitySnapshot(out, logPath)
		if err != nil {
			return err
		}
		arguments = append(arguments, "-db=file://"+filepath.ToSlash(db))
	}
	if *binary != "" {
		arguments = append(arguments, "-mode=binary", *binary)
	} else {
		arguments = append(arguments, "-tags="+project.RuntimeTags)
		arguments = append(arguments, strings.Fields(*packages)...)
	}
	fmt.Fprintf(out, "== go %s\n", strings.Join(arguments, " "))
	cmd := exec.Command("go", arguments...)
	cmd.Env = proxyProbeEnv(os.Environ(), map[string]string{"GOFLAGS": "-mod=vendor", "GOWORK": "off"})
	cmd.Stdout, cmd.Stderr = result, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("漏洞检查未通过；结果 %s，日志 %s: %w", jsonPath, logPath, err)
	}
	if err := result.Close(); err != nil {
		return err
	}
	// govulncheck's streaming JSON mode does not fail solely because it emits
	// findings. Source traces indicate reachability; a binary scan only proves
	// symbol presence and may include unused code without a call graph.
	file, err := os.Open(jsonPath)
	if err != nil {
		return err
	}
	defer file.Close()
	reachable, err := securityFindings(file)
	if err != nil {
		return err
	}
	label := "可达漏洞"
	if *binary != "" {
		label = "受影响符号告警"
		fmt.Fprintln(out, "二进制模式不提供调用图，符号存在不等于存在可利用路径。")
	}
	fmt.Fprintf(out, "%s %d 个；结果：%s（仅覆盖数据库已知问题）\n", label, len(reachable), jsonPath)
	if len(reachable) > 0 {
		for id := range reachable {
			fmt.Fprintln(out, id)
		}
		return errors.New("发现已知漏洞相关告警，需处理后重新扫描")
	}
	return nil
}

func securityFindings(reader io.Reader) (map[string]bool, error) {
	d := json.NewDecoder(reader)
	reachable := map[string]bool{}
	configured := false
	for {
		var message struct {
			Config  json.RawMessage `json:"config"`
			Finding *struct {
				OSV   string `json:"osv"`
				Trace []struct {
					Function string `json:"function"`
				} `json:"trace"`
			} `json:"finding"`
		}
		if err := d.Decode(&message); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("漏洞扫描结果不完整：%w", err)
		}
		configured = configured || len(message.Config) > 0 && string(message.Config) != "null"
		if f := message.Finding; f != nil && len(f.Trace) > 0 && f.Trace[0].Function != "" {
			reachable[f.OSV] = true
		}
	}
	if !configured {
		return nil, errors.New("漏洞扫描结果缺少扫描器配置，不能视为通过")
	}
	return reachable, nil
}

func securitySnapshot(out *tee, logPath string) (string, error) {
	directory, err := filepath.Abs(strings.TrimSuffix(logPath, ".log") + "-db")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	archive := directory + ".zip"
	const source = "https://vuln.go.dev/vulndb.zip"
	fmt.Fprintln(out, "== 官方漏洞数据库快照："+source)
	if err := step(out, "curl", "--fail", "--location", "--silent", "--show-error", "--retry", "2", "--max-time", "90", "--output", archive, source); err != nil {
		return "", fmt.Errorf("漏洞数据库下载失败：%w", err)
	}
	content, err := os.ReadFile(archive)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	fmt.Fprintln(out, "database zip SHA256: "+hex.EncodeToString(sum[:]))
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	var total uint64
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if !strings.HasSuffix(name, ".json") || (!strings.HasPrefix(name, "index/") && !strings.HasPrefix(name, "ID/")) {
			continue
		}
		if strings.Contains(name, "..") || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
			return "", errors.New("漏洞数据库归档路径无效")
		}
		total += file.UncompressedSize64
		if total > 256<<20 {
			return "", errors.New("漏洞数据库归档超出上限")
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		input, err := file.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(input, 16<<20))
		_ = input.Close()
		if err != nil || uint64(len(data)) != file.UncompressedSize64 {
			return "", errors.New("漏洞数据库条目不完整或超出上限")
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "index", "db.json")); err != nil {
		return "", errors.New("归档缺少漏洞数据库索引")
	}
	return directory, nil
}
