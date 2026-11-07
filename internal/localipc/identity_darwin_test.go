package localipc

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProcessExecIdentityChild(t *testing.T) {
	mode := os.Getenv("SMALLSSH_EXEC_ID_TEST")
	if mode == "" {
		return
	}
	os.Exit(processExecIdentityChild(mode))
}

func processExecIdentityChild(mode string) int {
	reader := bufio.NewReader(os.Stdin)
	if mode == "after" {
		fd, err := strconv.Atoi(os.Getenv("SMALLSSH_EXEC_ID_FD"))
		if err != nil {
			return 1
		}
		file := os.NewFile(uintptr(fd), "inherited test socket")
		defer file.Close()
		fmt.Println("executed", os.Getpid())
		_, _ = reader.ReadString('\n')
		return 0
	}
	connection, err := Dial(context.Background(), os.Getenv("SMALLSSH_EXEC_ID_DIR"))
	if err != nil {
		return 1
	}
	defer connection.Close()
	file, err := connection.(*net.UnixConn).File()
	if err != nil {
		return 1
	}
	defer file.Close()
	// Deliberately defeat the SDK's normal close-on-exec to exercise the OS
	// identity boundary even when a raw socket survives an identical self-exec.
	if _, err = unix.FcntlInt(file.Fd(), unix.F_SETFD, 0); err != nil {
		return 1
	}
	fmt.Println("connected")
	if _, err = reader.ReadString('\n'); err != nil {
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		return 1
	}
	if err := unix.Exec(executable, []string{executable, "-test.run=^TestProcessExecIdentityChild$"}, []string{"SMALLSSH_EXEC_ID_TEST=after", "SMALLSSH_EXEC_ID_FD=" + strconv.Itoa(int(file.Fd()))}); err != nil {
		return 1
	}
	return 1
}

func TestAuditIdentityRejectsSamePIDAndExecutableAfterExec(t *testing.T) {
	dir, err := os.MkdirTemp("", "ipc-exec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessExecIdentityChild$")
	command.Env = []string{"SMALLSSH_EXEC_ID_TEST=before", "SMALLSSH_EXEC_ID_DIR=" + dir}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); _ = command.Process.Kill(); _ = command.Wait() })
	reader := bufio.NewReader(output)
	line, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "connected" {
		t.Fatal("helper did not connect", line, err)
	}
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := Identify(connection)
	if err != nil {
		t.Fatal(err)
	}
	defer identity.Close()
	if identity.PID != command.Process.Pid || identity.Verify() != nil {
		t.Fatal("initial helper identity invalid")
	}
	if _, err = fmt.Fprintln(input, "exec"); err != nil {
		t.Fatal(err)
	}
	line, err = reader.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != fmt.Sprint("executed ", identity.PID) {
		t.Fatal("fixture did not preserve PID across exec", line, err)
	}
	if err := identity.Verify(); err == nil {
		t.Fatal("same PID and same executable inherited the old process identity after exec")
	}
	connection.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var byte [1]byte
	_, err = connection.Read(byte[:])
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal("fixture socket closed; generation test did not preserve the connection", err)
	}
}
