package httpwire

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func getBody(r *bufio.Reader, w io.Writer, path string) (int, string, error) {
	if _, err := fmt.Fprintf(w, "GET %s HTTP/1.1\r\nHost: x\r\n\r\n", path); err != nil {
		return 0, "", err
	}
	line, err := r.ReadString('\n')
	if err != nil || len(line) < 12 {
		return 0, "", fmt.Errorf("status line %q: %v", line, err)
	}
	var code, length int
	fmt.Sscanf(line[9:12], "%d", &code)
	for {
		h, err := r.ReadString('\n')
		if err != nil {
			return 0, "", err
		}
		if h == "\r\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(h), "content-length:") {
			fmt.Sscanf(strings.TrimSpace(h[15:]), "%d", &length)
		}
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, "", err
	}
	return code, string(b), nil
}

// http.hold and http.reply: every held request gets the hook's answer and
// nothing else, a second ticket, a reply outside the hook, a repeated reply and
// a ticket from an earlier batch are all ignored, and a request the hook leaves
// alone gets its handler's fallback. Under io_uring, requests share batches.
func TestHoldReply(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the server runtime's epoll and io_uring loops are Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "hold.dx"))
	bin := buildServer(t, dex, src, dir)
	for _, mode := range []string{"pool", "inline", "uring"} {
		t.Run(mode, func(t *testing.T) {
			addr := startServer(t, bin, mode)
			clients := 64
			if mode == "pool" {
				clients = 1
			}
			const rounds = 40
			var wg sync.WaitGroup
			errs := make(chan string, clients)
			start := make(chan struct{})
			for c := 0; c < clients; c++ {
				wg.Add(1)
				go func(c int) {
					defer wg.Done()
					conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
					if err != nil {
						errs <- err.Error()
						return
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(30 * time.Second))
					r := bufio.NewReader(conn)
					<-start
					for i := 0; i < rounds; i++ {
						n := c*1000 + i
						code, body, err := getBody(r, conn, fmt.Sprintf("/sq?%d", n))
						want := fmt.Sprintf(`{"sq":%d,`, n*n)
						if err != nil || code != 200 || !strings.HasPrefix(body, want) {
							errs <- fmt.Sprintf("/sq?%d: %d %s %v", n, code, body, err)
							return
						}
						if i%10 == 0 {
							code, body, err = getBody(r, conn, "/skip")
							if err != nil || code != 418 || body != `{"fallback":true}` {
								errs <- fmt.Sprintf("/skip: %d %s %v", code, body, err)
								return
							}
						}
					}
				}(c)
			}
			close(start)
			wg.Wait()
			close(errs)
			for e := range errs {
				t.Fatal(e)
			}
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_, body, err := getBody(bufio.NewReader(conn), conn, "/stats")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "uring" && body == `{"maxBatch":1}` {
				t.Fatalf("io_uring never put two held requests in one batch: %s", body)
			}
		})
	}
}
