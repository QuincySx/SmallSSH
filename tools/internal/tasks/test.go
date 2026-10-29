// test runs the static checks and the tests from one entry point and keeps the
// full output in a log under build/logs, so a run can be repeated, quoted in a
// review, or compared with the previous one instead of living in a temporary path.
package tasks

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sshclient/tools/internal/project"
)

// pipeline is what test and check share: go vet, then go test with one log.
type pipeline struct {
	packages string
	race     bool
	short    bool
	vet      bool
	serial   bool
	timeout  time.Duration
}

func bindPipeline(fs *flag.FlagSet) *pipeline {
	p := &pipeline{}
	fs.StringVar(&p.packages, "pkg", "./...", "packages to check and test")
	fs.BoolVar(&p.race, "race", false, "run the tests with the race detector")
	fs.BoolVar(&p.short, "short", false, "skip tests that take longer")
	fs.BoolVar(&p.vet, "vet", true, "run go vet before the tests")
	fs.BoolVar(&p.serial, "serial", false, "test one package at a time; timing-sensitive tests assume a quiet machine")
	fs.DurationVar(&p.timeout, "timeout", 20*time.Minute, "test timeout")
	return p
}

func runTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	p := bindPipeline(fs)
	if e := parse(fs, args); e != nil {
		return e
	}
	out, logPath, e := openLog("test", p.race)
	if e != nil {
		return e
	}
	defer out.Close()
	return p.run(out, logPath)
}

// run is the pipeline itself: vet, then test, both on the vendored tree so a
// stray GOFLAGS cannot bypass the patched dependencies.
func (p *pipeline) run(out *tee, logPath string) error {
	if p.vet {
		fmt.Fprintf(out, "== go vet -mod=vendor %s\n", p.packages)
		if e := step(out, "go", "vet", "-mod=vendor", p.packages); e != nil {
			fmt.Fprintf(out, "\n静态检查失败：%s\n", logPath)
			return errors.New("go vet 失败")
		}
	}
	args := []string{"test", "-mod=vendor", "-count=1", "-timeout", p.timeout.String()}
	if p.serial {
		// Several suites assert on timing windows; running packages in parallel
		// makes those windows depend on what else the machine is doing.
		args = append(args, "-p", "1")
	}
	if p.race {
		args = append(args, "-race")
	}
	if p.short {
		args = append(args, "-short")
	}
	fmt.Fprintf(out, "\n== go %s %s\n", strings.Join(args, " "), p.packages)
	if e := step(out, "go", append(args, p.packages)...); e != nil {
		fmt.Fprintf(out, "\n测试失败：%s\n", logPath)
		return errors.New("go test 失败")
	}
	fmt.Fprintf(out, "\n全部通过：%s\n", logPath)
	return nil
}

// openLog makes the run's log under build/logs, which is ignored like the rest
// of build/, and returns the writer that feeds both it and the terminal.
func openLog(prefix string, race bool) (*tee, string, error) {
	if _, e := project.Chdir(); e != nil {
		return nil, "", e
	}
	logDir := filepath.Join("build", "logs")
	if e := os.MkdirAll(logDir, 0o755); e != nil {
		return nil, "", e
	}
	name := prefix + "-" + time.Now().Format("20060102-150405")
	if race {
		name += "-race"
	}
	logPath := filepath.Join(logDir, name+".log")
	log, e := os.Create(logPath)
	if e != nil {
		return nil, "", e
	}
	return &tee{file: log, stdout: os.Stdout}, logPath, nil
}

// tee writes to the log and the terminal, so the log stays authoritative while
// the run is still visible.
type tee struct {
	file, stdout *os.File
}

func (t *tee) Write(b []byte) (int, error) {
	if _, e := t.stdout.Write(b); e != nil {
		return 0, e
	}
	return t.file.Write(b)
}

// Close closes the log file; the terminal stays open.
func (t *tee) Close() error { return t.file.Close() }

// step runs one command, sending its output through the log.
func step(out *tee, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}
