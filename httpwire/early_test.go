package httpwire

import (
	"bufio"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Under io_uring a connection's receive stays armed while its request waits
// for an answer, so the next request can arrive before the response leaves.
// It is held until then and answered after, and a client that hangs up while
// its request is parked costs nobody else anything.
func TestRequestDuringResponse(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the server runtime's epoll and io_uring loops are Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "later.dx"))
	bin := buildServer(t, dex, src, dir)
	for _, mode := range []string{"pool", "uring", "gather"} {
		t.Run(mode, func(t *testing.T) {
			addr := startServer(t, bin, mode)
			dial := func() (net.Conn, *bufio.Reader) {
				c, err := net.DialTimeout("tcp", addr, 5*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				c.SetDeadline(time.Now().Add(15 * time.Second))
				return c, bufio.NewReader(c)
			}

			a, ra := dial()
			defer a.Close()
			for i := 0; i < 3; i++ {
				if _, err := a.Write([]byte("GET /forever HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
					t.Fatal(err)
				}
				time.Sleep(100 * time.Millisecond)
				if _, err := a.Write([]byte("GET /fast HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
					t.Fatal(err)
				}
				first, err := gzRead(ra)
				if err != nil {
					t.Fatalf("round %d, /forever: %v", i, err)
				}
				second, err := gzRead(ra)
				if err != nil {
					t.Fatalf("round %d, /fast: %v", i, err)
				}
				if first.code != 200 || second.code != 200 || second.body != "fast" {
					t.Fatalf("round %d: %d %q then %d %q, want 200 then 200 \"fast\"", i, first.code, first.body, second.code, second.body)
				}
			}

			for i := 0; i < 20; i++ {
				c, _ := dial()
				c.Write([]byte("GET /forever HTTP/1.1\r\nHost: x\r\n\r\n"))
				c.Close()
			}
			b, rb := dial()
			defer b.Close()
			if got, err := gzGet(rb, b, "/fast", ""); err != nil || got.code != 200 || got.body != "fast" {
				t.Fatalf("after clients hung up: %v %d %q", err, got.code, got.body)
			}
			time.Sleep(1500 * time.Millisecond)
			if got, err := gzGet(rb, b, "/wait?2", ""); err != nil || got.code != 200 {
				t.Fatalf("once their requests ran out: %v %d %q", err, got.code, got.body)
			}
		})
	}
}
