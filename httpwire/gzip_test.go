package httpwire

import (
	"bufio"
	"bytes"
	"compress/gzip"
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

type gzResp struct {
	code     int
	encoding string
	vary     string
	body     string
}

func gzGet(r *bufio.Reader, w io.Writer, path, accept string) (gzResp, error) {
	req := "GET " + path + " HTTP/1.1\r\nHost: x\r\n"
	if accept != "" {
		req += "Accept-Encoding: " + accept + "\r\n"
	}
	if _, err := io.WriteString(w, req+"\r\n"); err != nil {
		return gzResp{}, err
	}
	line, err := r.ReadString('\n')
	if err != nil || len(line) < 12 {
		return gzResp{}, fmt.Errorf("status line %q: %v", line, err)
	}
	var out gzResp
	var length int
	fmt.Sscanf(line[9:12], "%d", &out.code)
	for {
		h, err := r.ReadString('\n')
		if err != nil {
			return gzResp{}, err
		}
		if h == "\r\n" {
			break
		}
		k, v, _ := strings.Cut(strings.TrimSpace(h), ":")
		v = strings.TrimSpace(v)
		switch strings.ToLower(k) {
		case "content-length":
			fmt.Sscanf(v, "%d", &length)
		case "content-encoding":
			out.encoding = v
		case "vary":
			out.vary = v
		}
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(r, b); err != nil {
		return gzResp{}, err
	}
	if out.encoding == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return gzResp{}, fmt.Errorf("gzip header: %v", err)
		}
		plain, err := io.ReadAll(zr)
		if err != nil {
			return gzResp{}, fmt.Errorf("gzip body: %v", err)
		}
		b = plain
	}
	out.body = string(b)
	return out, nil
}

func gzWant(n int) string {
	var s strings.Builder
	s.WriteString(`{"items":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			s.WriteString(",")
		}
		fmt.Fprintf(&s, `{"id":%d,"name":"item number %d"}`, i, i)
	}
	s.WriteString("]}")
	return s.String()
}

// http.gzip compresses large 2xx bodies for clients that accept gzip, and only
// those: no Accept-Encoding, q=0, a small body or an error status go out
// plain. Under http.gather the io_uring loop still answers every request on
// many keep-alive connections at once.
func TestGzipAndGather(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the server runtime's epoll and io_uring loops are Linux only")
	}
	if exec.Command("pkg-config", "--exists", "libdeflate").Run() != nil {
		t.Skip("libdeflate is not installed, so http.gzip is a no-op")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "gzip.dx"))
	bin := buildServer(t, dex, src, dir)
	for _, mode := range []string{"pool", "inline", "uring", "gather"} {
		t.Run(mode, func(t *testing.T) {
			addr := startServer(t, bin, mode)
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			r := bufio.NewReader(conn)
			cases := []struct {
				path, accept string
				gz           bool
			}{
				{"/big?100", "gzip", true},
				{"/big?100", "gzip, deflate, br", true},
				{"/big?100", "br;q=1.0, GZIP;q=0.5", true},
				{"/big?100", "", false},
				{"/big?100", "deflate", false},
				{"/big?100", "gzip;q=0", false},
				{"/big?100", "gzip; q=0.000", false},
				{"/big?100", "x-gzip", false},
				{"/big?2", "gzip", false},
				{"/missing", "gzip", false},
			}
			for _, c := range cases {
				got, err := gzGet(r, conn, c.path, c.accept)
				if err != nil {
					t.Fatalf("%s %q: %v", c.path, c.accept, err)
				}
				if (got.encoding == "gzip") != c.gz {
					t.Errorf("%s %q: Content-Encoding %q, want gzip=%v", c.path, c.accept, got.encoding, c.gz)
				}
				if c.gz && got.vary != "Accept-Encoding" {
					t.Errorf("%s %q: Vary %q", c.path, c.accept, got.vary)
				}
				if c.path == "/missing" {
					if got.code != 404 || len(got.body) < 2000 {
						t.Errorf("/missing: %d, %d bytes", got.code, len(got.body))
					}
					continue
				}
				var n int
				fmt.Sscanf(c.path, "/big?%d", &n)
				if got.code != 200 || got.body != gzWant(n) {
					t.Errorf("%s %q: %d, body differs (%d bytes)", c.path, c.accept, got.code, len(got.body))
				}
			}
			if mode == "pool" {
				return
			}
			clients, rounds := 48, 30
			var wg sync.WaitGroup
			errs := make(chan string, clients)
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
					conn.SetDeadline(time.Now().Add(20 * time.Second))
					r := bufio.NewReader(conn)
					for i := 0; i < rounds; i++ {
						n := 1 + (c*rounds+i)%60
						accept := ""
						if i%2 == 0 {
							accept = "gzip"
						}
						got, err := gzGet(r, conn, fmt.Sprintf("/big?%d", n), accept)
						if err != nil {
							errs <- err.Error()
							return
						}
						if got.code != 200 || got.body != gzWant(n) {
							errs <- fmt.Sprintf("client %d round %d: %d, wrong body", c, i, got.code)
							return
						}
					}
				}(c)
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				t.Error(e)
			}
		})
	}
}
