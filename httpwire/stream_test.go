package httpwire

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each piece is flushed on its own with a pause after it, so the client reads
// it as a separate chunk: events split mid-line, a CRLF split between its CR
// and its LF, and lone CRs are all what a real stream can deliver.
var ssePieces = []string{
	": comment\n",
	"data: hel", "lo\n\n",
	"data: line one\r\ndata: line two\r\n\r\n",
	"data:nospace\r", "\n\r", "\n",
	"event: ping\nid: 7\n\n",
	"data\n\n",
	"data: cr only\r\r",
	"data: {\"t\":\"Mepɛ\"}\n\n",
	"data: unfinished",
}

func flushPieces(w http.ResponseWriter, pieces []string) {
	f := w.(http.Flusher)
	for _, p := range pieces {
		io.WriteString(w, p)
		f.Flush()
		time.Sleep(15 * time.Millisecond)
	}
}

func streamServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushPieces(w, ssePieces)
	})
	mux.HandleFunc("/raw", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || string(body) != `{"q":1}` || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(400)
			io.WriteString(w, "bad request: "+r.Method+" "+string(body))
			return
		}
		flushPieces(w, []string{"ab", "cd", "ef"})
	})
	mux.HandleFunc("/err", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		io.WriteString(w, `{"error":"bad key"}`)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushPieces(w, []string{"data: one\n\n"})
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	return httptest.NewServer(mux)
}

// http.stream and http.streamEvents: the client half only, so it runs
// wherever libcurl does, not just on Linux.
func TestStream(t *testing.T) {
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", "stream.dx"))

	srv := streamServer()
	defer srv.Close()

	// Under the memory checker too, failing on anything left alive: the
	// callback's chunks, a rethrown exception and the abandoned transfers are
	// exactly where an ownership mistake would hide.
	for _, mode := range []string{"plain", "memcheck"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "memcheck" {
				t.Setenv("DEX_MEM_CHECK", "1")
			}
			bin := buildServer(t, dex, src, t.TempDir())
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "BASE="+srv.URL, "DEX_MEM_LEAKS=error")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			checkStreamOutput(t, string(out))
		})
	}
}

func checkStreamOutput(t *testing.T, out string) {
	t.Helper()
	events := "[hello][line one\nline two][nospace][][cr only][{\"t\":\"Mepɛ\"}]"
	want := strings.Join([]string{
		"events status=200 count=6 seen=" + events + " body=",
		"raw status=200 body= joined=abcdef",
		`error status=401 count=0 seen= body={"error":"bad key"}`,
		"stop status=200 count=2 seen=[hello][line one\nline two] body=",
		"throw caught=callback threw at 2 count=2 seen=[hello]",
		"after status=200 count=6 seen=" + events + " body=",
		"slow status=0 count=1 seen=[one] failed=true",
		"dead status=0 count=0 failed=true",
	}, "\n") + "\n"
	if out != want {
		t.Errorf("output differs\ngot:\n%s\nwant:\n%s", out, want)
	}
}
