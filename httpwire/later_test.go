package httpwire

import (
	"bufio"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// http.later parks a request and runs its handler again on the next pass,
// while the loop keeps answering other connections; it gives up after a
// second. Other backends refuse it and the handler answers at once.
func TestLater(t *testing.T) {
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
			parks := mode != "pool"
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
			for _, n := range []int{1, 2, 7} {
				got, err := gzGet(ra, a, "/wait?"+strconv.Itoa(n), "")
				if err != nil {
					t.Fatal(err)
				}
				want := "1"
				if parks {
					want = strconv.Itoa(n)
				}
				if got.code != 200 || got.body != want {
					t.Errorf("/wait?%d: %d %q, want 200 %q", n, got.code, got.body, want)
				}
			}
			if got, err := gzGet(ra, a, "/twice", ""); err != nil || got.code != 200 || got.body != "false" {
				t.Errorf("/twice: %v %d %q, want 200 \"false\"", err, got.code, got.body)
			}

			// /forever parks for a second on a; b is answered meanwhile.
			b, rb := dial()
			defer b.Close()
			done := make(chan gzResp, 1)
			start := time.Now()
			go func() {
				r, err := gzGet(ra, a, "/forever", "")
				if err != nil {
					t.Error(err)
				}
				done <- r
			}()
			time.Sleep(50 * time.Millisecond)
			for i := 0; i < 20; i++ {
				t0 := time.Now()
				r, err := gzGet(rb, b, "/fast", "")
				if err != nil {
					t.Fatal(err)
				}
				if r.code != 200 || r.body != "fast" {
					t.Fatalf("/fast: %d %q", r.code, r.body)
				}
				if d := time.Since(t0); d > 200*time.Millisecond {
					t.Errorf("/fast took %v while a request was parked", d)
				}
			}
			r := <-done
			el := time.Since(start)
			n, _ := strconv.Atoi(r.body)
			if r.code != 200 {
				t.Fatalf("/forever: %d %q", r.code, r.body)
			}
			if parks && (el < 900*time.Millisecond || el > 3*time.Second || n < 10) {
				t.Errorf("/forever: answered after %v and %d runs; want about 1 s and many runs", el, n)
			}
			if !parks && (n != 1 || el > 500*time.Millisecond) {
				t.Errorf("/forever without uring: %d runs after %v; want 1 at once", n, el)
			}
			// The connection is still usable afterwards.
			if r, err := gzGet(ra, a, "/wait?3", ""); err != nil || r.code != 200 {
				t.Errorf("after /forever: %v %d %q", err, r.code, r.body)
			}
		})
	}
}
