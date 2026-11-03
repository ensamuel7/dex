package httpwire

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLanes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("lanes and the io_uring loop are Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "lanes.dx"))
	bin := buildServer(t, dex, src, dir)
	t.Setenv("WINDOW", "40")
	for _, mode := range []string{"inline", "uring"} {
		t.Run(mode, func(t *testing.T) {
			addr := startServer(t, bin, mode)
			const clients = 48
			const rounds = 20
			var wg sync.WaitGroup
			var mu sync.Mutex
			crossWorker := false
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
						if err != nil || code != 200 || !strings.HasPrefix(body, fmt.Sprintf(`{"sq":%d,`, n*n)) {
							errs <- fmt.Sprintf("/sq?%d: %d %s %v", n, code, body, err)
							return
						}
						if mode == "uring" {
							if !strings.Contains(body, fmt.Sprintf(`"lane":%d,`, n%2)) {
								errs <- fmt.Sprintf("/sq?%d answered by the wrong lane: %s", n, body)
								return
							}
							if strings.Contains(body, `"workers":2`) {
								mu.Lock()
								crossWorker = true
								mu.Unlock()
							}
						} else if body != fmt.Sprintf(`{"sq":%d,"direct":true}`, n*n) {
							errs <- fmt.Sprintf("/sq?%d without io_uring: %s", n, body)
							return
						}
						if i%5 == 0 {
							code, body, err = getBody(r, conn, "/skip")
							if err != nil || code != 500 || body != `{"error":"internal server error"}` {
								errs <- fmt.Sprintf("/skip: %d %s %v", code, body, err)
								return
							}
							code, body, err = getBody(r, conn, "/missing")
							if err != nil || code != 200 || body != `{"missing":true}` {
								errs <- fmt.Sprintf("/missing: %d %s %v", code, body, err)
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
			if mode == "uring" && !crossWorker {
				t.Fatal("no lane batch ever held requests from both workers")
			}
		})
	}
}
