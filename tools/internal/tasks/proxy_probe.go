package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sshclient/tools/internal/project"
)

// runProxyProbe measures a separate, explicitly pinned module. It neither
// changes the application's module/vendor tree nor executes the linked probe.
func runProxyProbe(args []string) error {
	fs := flag.NewFlagSet("proxy-probe", flag.ContinueOnError)
	platforms := fs.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "comma-separated targets, or all")
	if err := parse(fs, args); err != nil {
		return err
	}
	root, err := project.Chdir()
	if err != nil {
		return err
	}
	targets, err := parseTargets(*platforms)
	if err != nil {
		return err
	}
	if err := verifyVendorPatch(); err != nil {
		return err
	}
	out, logPath, err := openLog("proxy-probe", false)
	if err != nil {
		return err
	}
	defer out.Close()
	work := filepath.Join(root, "build", "proxy-probe", time.Now().Format("20060102-150405"))
	module := filepath.Join(work, "module")
	if err := os.MkdirAll(module, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(root, "tools", "proxy-probe", "fixture"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, "tools", "proxy-probe", "fixture", entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(module, entry.Name()), content, 0644); err != nil {
			return err
		}
	}
	// An isolated build cache makes first/repeat timings distinguishable. This
	// fixture has its own go.mod and never falls back to the app's vendor tree.
	cache := filepath.Join(work, "cache")
	if err := os.MkdirAll(cache, 0755); err != nil {
		return err
	}
	env := proxyProbeEnv(os.Environ(), map[string]string{
		"GOWORK": "off", "GOFLAGS": "", "CGO_ENABLED": "0", "GOCACHE": cache,
		"GIT_TERMINAL_PROMPT": "0", "GOTOOLCHAIN": "local",
	})
	run := func(environment []string, arguments ...string) (time.Duration, error) {
		fmt.Fprintf(out, "== go %s\n", strings.Join(arguments, " "))
		cmd := exec.Command("go", arguments...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = module, environment, out, out
		start := time.Now()
		err := cmd.Run()
		elapsed := time.Since(start)
		fmt.Fprintf(out, "elapsed: %s\n", elapsed.Round(time.Millisecond))
		return elapsed, err
	}
	setup, err := run(env, "mod", "tidy")
	if err != nil {
		return fmt.Errorf("代理探针依赖准备失败，日志 %s: %w", logPath, err)
	}
	type measurement struct {
		Profile       string        `json:"profile"`
		Platform      string        `json:"platform"`
		Bytes         int64         `json:"bytes"`
		GzipBytes     int64         `json:"gzipBytes"`
		FirstBuild    time.Duration `json:"firstBuildNanoseconds"`
		RepeatBuild   time.Duration `json:"repeatBuildNanoseconds"`
		Packages      int           `json:"packages"`
		TailscalePkgs int           `json:"tailscalePackages"`
		GVisorPkgs    int           `json:"gvisorPackages"`
	}
	var measurements []measurement
	for _, target := range targets {
		for _, profile := range []struct{ name, tags string }{
			{"baseline", "probe_baseline"},
			{"outbound", "with_utls"},
			{"outbound-hy2", "with_utls,with_quic"},
		} {
			directory := filepath.Join(work, target.goos+"-"+target.goarch)
			if err := os.MkdirAll(directory, 0755); err != nil {
				return err
			}
			binary := filepath.Join(directory, profile.name)
			if target.goos == "windows" {
				binary += ".exe"
			}
			environment := proxyProbeEnv(env, map[string]string{"GOOS": target.goos, "GOARCH": target.goarch})
			arguments := []string{"build", "-mod=readonly", "-trimpath", "-tags", profile.tags,
				"-ldflags", "-s -w -checklinkname=0", "-o", binary, "."}
			first, err := run(environment, arguments...)
			if err != nil {
				return fmt.Errorf("%s %s 编译失败，日志 %s: %w", target, profile.name, logPath, err)
			}
			repeat, err := run(environment, arguments...)
			if err != nil {
				return err
			}
			stat, err := os.Stat(binary)
			if err != nil {
				return err
			}
			compressed, err := proxyProbeGzip(binary)
			if err != nil {
				return err
			}
			cmd := exec.Command("go", "list", "-mod=readonly", "-tags", profile.tags, "-deps", "-f", "{{.ImportPath}}", ".")
			cmd.Dir, cmd.Env, cmd.Stderr = module, environment, out
			deps, err := cmd.Output()
			if err != nil {
				return err
			}
			if err := os.WriteFile(binary+".deps.txt", deps, 0644); err != nil {
				return err
			}
			m := measurement{Profile: profile.name, Platform: target.String(), Bytes: stat.Size(),
				GzipBytes: compressed, FirstBuild: first, RepeatBuild: repeat}
			for _, p := range strings.Fields(string(deps)) {
				m.Packages++
				if strings.Contains(p, "tailscale") {
					m.TailscalePkgs++
				}
				if strings.Contains(p, "gvisor") {
					m.GVisorPkgs++
				}
			}
			measurements = append(measurements, m)
			fmt.Fprintf(out, "%s %s: %.2f MiB; gzip %.2f MiB; first %s; repeat %s; packages %d\n",
				target, profile.name, float64(m.Bytes)/(1<<20), float64(compressed)/(1<<20),
				first.Round(time.Millisecond), repeat.Round(time.Millisecond), m.Packages)
		}
	}
	report := struct {
		Setup        time.Duration `json:"setupNanoseconds"`
		GoVersion    string        `json:"goVersion"`
		Log          string        `json:"log"`
		Limitations  string        `json:"limitations"`
		Measurements any           `json:"measurements"`
	}{setup, runtime.Version(), logPath,
		"Compile only; no probe executed, no real server contacted. Standalone binaries include the runtime and shared dependencies; sizes are not application-package increments. Profiles share the new cache in build order. gzip is a single executable, not an installer.", measurements}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	reportPath := filepath.Join(work, "report.json")
	if err := os.WriteFile(reportPath, append(encoded, '\n'), 0644); err != nil {
		return err
	}
	fmt.Fprintf(out, "报告：%s\n", reportPath)
	return nil
}

func proxyProbeEnv(environment []string, overrides map[string]string) []string {
	result := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func proxyProbeGzip(path string) (int64, error) {
	input, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer input.Close()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := io.Copy(writer, input); err != nil {
		_ = writer.Close()
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	if err := os.WriteFile(path+".gz", compressed.Bytes(), 0644); err != nil {
		return 0, err
	}
	return int64(compressed.Len()), nil
}
