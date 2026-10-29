package httpwire

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current runtime")

type wireCase struct {
	name     string
	segments []string
}

func big(n int) string { return strings.Repeat("a", n) }

var cases = []wireCase{
	{"simple", []string{"GET /echo HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"query", []string{"GET /echo?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"bare query mark", []string{"GET /echo? HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"param", []string{"GET /items/42 HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"param empty", []string{"GET /items/ HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"not found", []string{"GET /nope HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"wrong method", []string{"DELETE /echo HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"lowercase method", []string{"get /echo HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"extra headers", []string{"GET /cookies HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"keep alive twice", []string{"GET /echo HTTP/1.1\r\nHost: x\r\n\r\n", "GET /echo?n=2 HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"pipelined", []string{"GET /echo?n=1 HTTP/1.1\r\nHost: x\r\n\r\nGET /echo?n=2 HTTP/1.1\r\nHost: x\r\n\r\n"}},
	{"pipelined post", []string{"POST /echo HTTP/1.1\r\nContent-Length: 3\r\n\r\nabcGET /echo?n=2 HTTP/1.1\r\n\r\n"}},
	{"split request line", []string{"GET /ec", "ho HTTP/1.1\r\nHo", "st: x\r\n\r\n"}},
	{"split terminator", []string{"GET /echo HTTP/1.1\r\nHost: x\r\n\r", "\n"}},
	{"post body", []string{"POST /echo HTTP/1.1\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello"}},
	{"post body split", []string{"POST /echo HTTP/1.1\r\nContent-Length: 5\r\n\r\nhe", "llo"}},
	{"post body longer than length", []string{"POST /echo HTTP/1.1\r\nContent-Length: 2\r\n\r\nhello"}},
	{"zero length", []string{"POST /echo HTTP/1.1\r\nContent-Length: 0\r\n\r\n"}},
	{"duplicate length same", []string{"POST /echo HTTP/1.1\r\nContent-Length: 3\r\nContent-Length: 3\r\n\r\nabc"}},
	{"duplicate length differs", []string{"POST /echo HTTP/1.1\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\nabcd"}},
	{"length not digits", []string{"POST /echo HTTP/1.1\r\nContent-Length: 3a\r\n\r\nabc"}},
	{"length negative", []string{"POST /echo HTTP/1.1\r\nContent-Length: -1\r\n\r\n"}},
	{"length empty", []string{"POST /echo HTTP/1.1\r\nContent-Length:\r\n\r\n"}},
	{"length ten digits", []string{"POST /echo HTTP/1.1\r\nContent-Length: 1000000000\r\n\r\n"}},
	{"length over body limit", []string{"POST /echo HTTP/1.1\r\nContent-Length: 2048\r\n\r\n"}},
	{"length lowercase name", []string{"POST /echo HTTP/1.1\r\ncontent-length: 2\r\n\r\nhi"}},
	{"chunked", []string{"POST /echo HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n0\r\n\r\n"}},
	{"chunked lowercase", []string{"POST /echo HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n0\r\n\r\n"}},
	{"chunked and length", []string{"POST /echo HTTP/1.1\r\nContent-Length: 4\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"}},
	{"header names any case", []string{"GET /echo HTTP/1.1\r\nAUTHORIZATION: Bearer abc\r\nContent-TYPE: text/x\r\n\r\n"}},
	{"duplicate header last wins", []string{"GET /echo HTTP/1.1\r\nX-Dup: one\r\nx-dup: two\r\n\r\n"}},
	{"header value whitespace", []string{"GET /echo HTTP/1.1\r\nAuthorization: \t  Bearer abc  \r\n\r\n"}},
	{"header empty value", []string{"GET /echo HTTP/1.1\r\nAuthorization:\r\n\r\n"}},
	{"header without colon", []string{"GET /echo HTTP/1.1\r\nnot a header\r\nAuthorization: yes\r\n\r\n"}},
	{"header bare newlines", []string{"GET /echo HTTP/1.1\nAuthorization: lf\n\n"}},
	{"header long name", []string{"GET /echo HTTP/1.1\r\n" + big(300) + ": v\r\nAuthorization: after\r\n\r\n"}},
	{"many headers", []string{"GET /echo HTTP/1.1\r\n" + strings.Repeat("X-A: 1\r\n", 40) + "X-Dup: last\r\n\r\n"}},
	{"http 1.0", []string{"GET /echo HTTP/1.0\r\nHost: x\r\n\r\n"}},
	{"http 1.0 keep alive header", []string{"GET /echo HTTP/1.0\r\nConnection: keep-alive\r\n\r\n"}},
	{"version text in path", []string{"GET /HTTP/1.1 HTTP/1.0\r\n\r\n"}},
	{"connection close", []string{"GET /echo HTTP/1.1\r\nConnection: close\r\n\r\nGET /echo?n=2 HTTP/1.1\r\n\r\n"}},
	{"connection close upper", []string{"GET /echo HTTP/1.1\r\nConnection: CLOSE\r\n\r\n"}},
	{"connection close list", []string{"GET /echo HTTP/1.1\r\nConnection: close, x\r\n\r\n"}},
	{"leading whitespace", []string{"  GET /echo HTTP/1.1\r\n\r\n"}},
	{"tab separated", []string{"GET\t/echo\tHTTP/1.1\r\n\r\n"}},
	{"no version", []string{"GET /echo\r\n\r\n"}},
	{"long path", []string{"GET /" + big(3000) + " HTTP/1.1\r\n\r\n"}},
	{"long method", []string{big(40) + " /echo HTTP/1.1\r\n\r\n"}},
	{"header block too large", []string{"GET /echo HTTP/1.1\r\nX-Big: " + big(40000) + "\r\n\r\n"}},
	{"unicode body", []string{"POST /echo HTTP/1.1\r\nContent-Length: 13\r\n\r\n\"héé\" \\ \x01"}},
}

func buildServer(t *testing.T, dex, src, dir string) string {
	cmd := exec.Command(dex, "build", "--no-lint", src)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", src, err, out)
	}
	return filepath.Join(dir, "build", strings.TrimSuffix(filepath.Base(src), ".dx"))
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startServer(t *testing.T, bin, mode string) string {
	port := freePort(t)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "MODE="+mode, fmt.Sprintf("PORT=%d", port))
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("server output:\n%s", logs.String())
		}
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server %s did not start:\n%s", mode, logs.String())
	return ""
}

func exchange(t *testing.T, addr string, c wireCase) string {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i, s := range c.segments {
		if i > 0 {
			time.Sleep(40 * time.Millisecond)
		}
		if _, err := conn.Write([]byte(s)); err != nil {
			return fmt.Sprintf("write error after %d segments\n", i)
		}
	}
	var got bytes.Buffer
	buf := make([]byte, 65536)
	for {
		conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, err := conn.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				got.WriteString("\n<open>")
			} else {
				got.WriteString("\n<closed>")
			}
			return got.String()
		}
	}
}

func transcript(t *testing.T, addr string) string {
	var out strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&out, "=== %s\n%s\n", c.name, exchange(t, addr, c))
	}
	return out.String()
}

func TestWire(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the server runtime's epoll and io_uring loops are Linux only")
	}
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	for _, server := range []string{"echo_full", "echo_gets"} {
		src, _ := filepath.Abs(filepath.Join("testdata", server+".dx"))
		bin := buildServer(t, dex, src, dir)
		for _, mode := range []string{"pool", "inline", "uring"} {
			t.Run(server+"/"+mode, func(t *testing.T) {
				got := transcript(t, startServer(t, bin, mode))
				golden := filepath.Join("testdata", server+"."+mode+".golden")
				if *update {
					if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					gotParts := strings.Split(got, "\n=== ")
					wantParts := strings.Split(string(want), "\n=== ")
					for i := 0; i < len(gotParts) && i < len(wantParts); i++ {
						if gotParts[i] != wantParts[i] {
							t.Errorf("first difference:\n--- want\n%s\n--- got\n%s", wantParts[i], gotParts[i])
							break
						}
					}
					if len(gotParts) != len(wantParts) {
						t.Errorf("case count: want %d, got %d", len(wantParts), len(gotParts))
					}
				}
			})
		}
	}
}
