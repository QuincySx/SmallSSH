package tasks

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"sshclient/tools/internal/project"
)

// compile verifies platform-specific Go code and produces a CLI for headless
// process-boundary tests, without packaging or launching the desktop app.
func runCompile(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	platforms := fs.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "comma-separated targets, or all")
	packages := fs.String("pkg", "./...", "packages to compile")
	if err := parse(fs, args); err != nil {
		return err
	}
	if _, err := project.Chdir(); err != nil {
		return err
	}
	targets, err := parseTargets(*platforms)
	if err != nil {
		return err
	}
	if err = verifyVendorPatch(); err != nil {
		return err
	}
	out, logPath, err := openLog("compile", false)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, target := range targets {
		directory := filepath.Join("build", "compile", target.goos+"-"+target.goarch)
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
		fmt.Fprintf(out, "== go build -mod=vendor -tags %s -ldflags %q -o %s/ %s (%s)\n", project.RuntimeTags, project.RuntimeLinkFlags, directory, *packages, target)
		command := exec.Command("go", "build", "-mod=vendor", "-tags", project.RuntimeTags, "-ldflags", project.RuntimeLinkFlags, "-o", directory+string(os.PathSeparator), *packages)
		command.Env = append(os.Environ(), "GOOS="+target.goos, "GOARCH="+target.goarch)
		if target.goos != runtime.GOOS || target.goarch != runtime.GOARCH {
			command.Env = append(command.Env, "CGO_ENABLED=0")
		}
		command.Stdout, command.Stderr = out, out
		if err := command.Run(); err != nil {
			return fmt.Errorf("%s 编译失败：%s: %w", target, logPath, err)
		}
	}
	fmt.Fprintln(out, "全部编译通过："+logPath)
	return nil
}
