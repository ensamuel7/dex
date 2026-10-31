package httpwire

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// One request on a fresh connection; the status code, or 0 if it failed.
func status(t *testing.T, method, url string) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	req.Close = true
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return 0
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func rowCount(t *testing.T, base string) int {
	t.Helper()
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(base + "/count")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	// {"n":N,"batches":M}
	s := string(b)
	i := strings.Index(s, `"n":`)
	j := strings.Index(s, `,"batches"`)
	if i < 0 || j < i {
		t.Fatalf("unexpected /count body %q", s)
	}
	n, err := strconv.Atoi(s[i+4 : j])
	if err != nil {
		t.Fatalf("unexpected /count body %q", s)
	}
	return n
}

// http.afterBatch: every 201 a client was given is in the database, a batch
// whose commit fails is answered 500 and leaves nothing behind, and the next
// batch commits normally. In every mode, since on epoll and the pool a batch
// is one request and the guarantee is the same.
func TestAfterBatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the server runtime's epoll and io_uring loops are Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "batch.dx"))
	bin := buildServer(t, dex, src, dir)
	for _, mode := range []string{"pool", "inline", "uring"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DB", filepath.Join(t.TempDir(), "t.db"))
			t.Setenv("MAXC", "0")
			base := "http://" + startServer(t, bin, mode)

			// The test program keeps a single-threaded connection and a
			// global transaction flag, so on the pool, where handlers run on
			// several threads, its requests go one at a time.
			parallel := 16
			if mode == "pool" {
				parallel = 1
			}
			const adds = 120
			var created int64
			var wg sync.WaitGroup
			sem := make(chan struct{}, parallel)
			for i := 0; i < adds; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					if status(t, "POST", fmt.Sprintf("%s/add?v%d", base, i)) == 201 {
						atomic.AddInt64(&created, 1)
					}
				}(i)
			}
			wg.Wait()
			if created != adds {
				t.Fatalf("%d of %d adds answered 201", created, adds)
			}
			if n := rowCount(t, base); n != adds {
				t.Fatalf("%d rows after %d acknowledged adds", n, adds)
			}

			if c := status(t, "POST", base+"/arm"); c != 200 {
				t.Fatalf("arm: %d", c)
			}
			if c := status(t, "POST", base+"/add?doomed"); c != 500 {
				t.Fatalf("add in a batch whose commit fails: got %d, want 500", c)
			}
			if n := rowCount(t, base); n != adds {
				t.Fatalf("a failed batch left %d rows, want %d", n, adds)
			}
			if c := status(t, "POST", base+"/add?after"); c != 201 {
				t.Fatalf("add after a failed batch: got %d, want 201", c)
			}
			if n := rowCount(t, base); n != adds+1 {
				t.Fatalf("%d rows, want %d", n, adds+1)
			}
		})
	}
}

// readResponse reads one whole response so the connection can be reused.
func readResponse(r *bufio.Reader) (int, error) {
	line, err := r.ReadString('\n')
	if err != nil || len(line) < 12 {
		return 0, fmt.Errorf("status line %q: %v", line, err)
	}
	code, _ := strconv.Atoi(line[9:12])
	length := 0
	for {
		h, err := r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		if h == "\r\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(h), "content-length:") {
			length, _ = strconv.Atoi(strings.TrimSpace(h[15:]))
		}
	}
	_, err = io.CopyN(io.Discard, r, int64(length))
	return code, err
}

// Under a low open-file limit one ring holds about a thousand connections, so
// a server asked for 3,000 opens more rings on the same thread, and every
// connection is served, twice, without any being turned away.
func TestUringRings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("io_uring is Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "batch.dx"))
	bin := buildServer(t, dex, src, dir)

	port := freePort(t)
	cmd := exec.Command("sh", "-c", "ulimit -n 1100 && exec "+bin)
	cmd.Env = append(os.Environ(), "MODE=uring", fmt.Sprintf("PORT=%d", port),
		"DB="+filepath.Join(dir, "t.db"), "MAXC=3000")
	var logs bytes.Buffer
	var mu sync.Mutex
	cmd.Stdout = &lockedWriter{w: &logs, mu: &mu}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		if i > 100 {
			t.Fatalf("server did not start:\n%s", logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	banner := logs.String()
	mu.Unlock()
	if strings.Contains(banner, "io_uring unavailable") {
		t.Skip("io_uring is not available here")
	}
	if !strings.Contains(banner, "rings)") {
		t.Fatalf("expected several rings, got: %s", banner)
	}

	const n = 2500
	conns := make([]net.Conn, n)
	readers := make([]*bufio.Reader, n)
	defer func() {
		for _, c := range conns {
			if c != nil {
				c.Close()
			}
		}
	}()
	for round := 1; round <= 2; round++ {
		var ok, failed int64
		var wg sync.WaitGroup
		sem := make(chan struct{}, 128)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if conns[i] == nil {
					c, err := net.DialTimeout("tcp", addr, 5*time.Second)
					if err != nil {
						atomic.AddInt64(&failed, 1)
						return
					}
					conns[i] = c
					readers[i] = bufio.NewReader(c)
				}
				conns[i].SetDeadline(time.Now().Add(15 * time.Second))
				fmt.Fprintf(conns[i], "GET /count HTTP/1.1\r\nHost: x\r\n\r\n")
				if code, err := readResponse(readers[i]); err == nil && code == 200 {
					atomic.AddInt64(&ok, 1)
				} else {
					atomic.AddInt64(&failed, 1)
				}
			}(i)
		}
		wg.Wait()
		if ok != n {
			t.Fatalf("round %d: %d of %d keep-alive connections answered (%d failed); %s", round, ok, n, failed, banner)
		}
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
