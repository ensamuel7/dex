package httpwire

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func ask(t *testing.T, addr string) string {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "GET /who HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	var got bytes.Buffer
	got.ReadFrom(conn)
	parts := strings.SplitN(got.String(), "\r\n\r\n", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "HTTP/1.1 200") {
		t.Fatalf("bad response from %s: %q", addr, got.String())
	}
	return parts[1]
}

func TestForkedWorkersShareBothPorts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the server runtime's epoll and io_uring loops are Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "workers.dx"))
	bin := buildServer(t, dex, src, dir)
	for _, mode := range []string{"pool", "inline", "uring"} {
		t.Run(mode, func(t *testing.T) {
			port, extra := freePort(t), freePort(t)
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "MODE="+mode, fmt.Sprintf("PORT=%d", port), fmt.Sprintf("EXTRA_PORT=%d", extra))
			var logs bytes.Buffer
			cmd.Stdout = &logs
			cmd.Stderr = &logs
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			main := fmt.Sprintf("127.0.0.1:%d", port)
			second := fmt.Sprintf("127.0.0.1:%d", extra)
			up := false
			for i := 0; i < 200 && !up; i++ {
				c1, e1 := net.Dial("tcp", main)
				c2, e2 := net.Dial("tcp", second)
				if e1 == nil {
					c1.Close()
				}
				if e2 == nil {
					c2.Close()
				}
				up = e1 == nil && e2 == nil
				time.Sleep(20 * time.Millisecond)
			}
			if !up {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatalf("not listening on both ports:\n%s", logs.String())
			}
			time.Sleep(200 * time.Millisecond)
			seen := map[string]bool{}
			for i := 0; i < 200; i++ {
				seen[ask(t, main)] = true
				seen[ask(t, second)] = true
			}
			for w := range seen {
				if w != "0" && w != "1" && w != "2" && w != "3" {
					t.Errorf("unexpected worker index %q", w)
				}
			}
			if len(seen) < 2 {
				t.Errorf("every request went to one process: %v", seen)
			}
			cmd.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				t.Fatal("did not exit on SIGTERM")
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				c, err := net.Dial("tcp", main)
				if err != nil {
					return
				}
				c.Close()
				time.Sleep(50 * time.Millisecond)
			}
			t.Errorf("a forked worker outlived SIGTERM to the original:\n%s", logs.String())
		})
	}
}
