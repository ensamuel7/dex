package codegen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A helper that allocates on behalf of its caller, a caller that allocates
// after the helper returns, an array and a map that grow, and a loop that
// churns temporaries — the cases the profiler's attribution has to get right.
const profileProgram = `
fn label(i: int): string {
    return "item-${i}"
}

fn main(): void {
    let names: string[] = []
    let index: map[string, int] = {}
    for (let i = 0; i < 2000; i++) {
        let name: string = label(i) + "!"
        names.push(name)
        index.set(name, i)
    }
    let churn: int = 0
    for (let i = 0; i < 5000; i++) {
        let s: string = label(i)
        churn = churn + s.len()
    }
    assert(names.len() == 2000)
    assert(churn > 0)
}
`

// Line numbers in profileProgram, counting the leading newline as line 1.
const (
	profLineLabel  = 3  // return "item-${i}"
	profLineNames  = 7  // let names: string[] = []
	profLineIndex  = 8  // let index: map[string, int] = {}
	profLineConcat = 10 // let name: string = label(i) + "!"
	profLinePush   = 11 // names.push(name)
	profLineSet    = 12 // index.set(name, i)
)

func withProfile(g *Generator) { g.EnableProfile() }

func TestProfileEmitsRuntimeMarkersAndFrames(t *testing.T) {
	out := generateWith(t, profileProgram, withProfile)
	assertContains(t, out, "#define DEX_PROFILE 1")
	assertContains(t, out, "#define DEX_AT(site) (dex_pf_site = (site))")
	assertNotContains(t, out, "#define DEX_MEM_CHECK 1")
	// The profiler leaves the object pool on: it measures the program as it
	// runs in a release build, not as the checker runs it.
	assertContains(t, out, "#define DEX_USE_POOL 1")

	mainIdx := strings.Index(out, "int main(")
	initIdx := strings.Index(out, "dex_pf_init();")
	firstMarker := strings.Index(out[mainIdx:], "DEX_AT(")
	if initIdx < mainIdx || firstMarker < 0 || mainIdx+firstMarker < initIdx {
		t.Fatalf("dex_pf_init() must precede the first statement marker in main")
	}
	// One frame per function, so a return hands the caller's line back.
	if n := strings.Count(out, "    DEX_PF_FRAME();\n"); n != 2 {
		t.Errorf("expected DEX_PF_FRAME() in both functions, got %d", n)
	}
}

func TestProfileOffByDefault(t *testing.T) {
	out := generate(t, profileProgram)
	assertNotContains(t, out, "#define DEX_PROFILE 1")
	assertNotContains(t, out, "DEX_AT(\"")
	assertNotContains(t, out, "dex_pf_init();")
	assertNotContains(t, out, "    DEX_PF_FRAME();\n")
	// The container hooks are still in the runtime source, and compile away.
	assertContains(t, out, "#define DEX_PF_BUF_SET(obj, bytes) ((void)0)")
}

type profileJSON struct {
	Total struct {
		Allocs      int64 `json:"allocs"`
		Frees       int64 `json:"frees"`
		AllocBytes  int64 `json:"alloc_bytes"`
		LiveObjects int64 `json:"live_objects"`
		LiveBytes   int64 `json:"live_bytes"`
		PeakBytes   int64 `json:"peak_bytes"`
	} `json:"total"`
	Sites []struct {
		Site       string `json:"site"`
		Allocs     int64  `json:"allocs"`
		Frees      int64  `json:"frees"`
		AllocBytes int64  `json:"alloc_bytes"`
		LiveBytes  int64  `json:"live_bytes"`
		PeakBytes  int64  `json:"peak_bytes"`
		AtPeak     int64  `json:"at_peak"`
	} `json:"sites"`
	Timeline [][3]float64 `json:"timeline"`
}

// Builds profileProgram with the profiler, runs it, and checks the numbers
// it writes rather than only that it writes something.
func TestProfileRunAttributesAllocations(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("cc not available")
	}
	var gen *Generator
	out := generateWith(t, profileProgram, func(g *Generator) { g.EnableProfile(); gen = g })

	dir := t.TempDir()
	src := filepath.Join(dir, "prof.c")
	bin := filepath.Join(dir, "prof")
	if err := os.WriteFile(src, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-o", bin, "-Werror=incompatible-pointer-types", "-Werror=implicit-function-declaration", src}, gen.CompilerFlags()...)
	if b, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compiling the profile build failed: %v\n%s", err, b)
	}
	jsonPath := filepath.Join(dir, "prof.json")
	run := exec.Command(bin)
	run.Env = append(os.Environ(), "DEX_PROFILE_OUT="+jsonPath, "DEX_PROFILE_QUIET=1")
	if b, err := run.CombinedOutput(); err != nil {
		t.Fatalf("running the profile build failed: %v\n%s", err, b)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("no profile written: %v", err)
	}
	var p profileJSON
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("profile is not valid JSON: %v\n%s", err, raw)
	}

	// Everything this program allocates it also frees, so the books balance.
	if p.Total.Allocs == 0 || p.Total.Allocs != p.Total.Frees {
		t.Errorf("allocs %d, frees %d: expected equal and non-zero", p.Total.Allocs, p.Total.Frees)
	}
	if p.Total.LiveObjects != 0 || p.Total.LiveBytes != 0 {
		t.Errorf("expected nothing live at exit, got %d objects, %d bytes", p.Total.LiveObjects, p.Total.LiveBytes)
	}
	var sumLive, sumAlloc int64
	for _, s := range p.Sites {
		sumLive += s.LiveBytes
		sumAlloc += s.AllocBytes
	}
	if sumLive != 0 || sumAlloc != p.Total.AllocBytes {
		t.Errorf("site totals disagree with the program total: live %d, allocated %d vs %d", sumLive, sumAlloc, p.Total.AllocBytes)
	}

	site := func(line int) (allocs, allocBytes, peak, atPeak int64) {
		suffix := ":" + itoa(line)
		for _, s := range p.Sites {
			if strings.HasSuffix(s.Site, suffix) {
				return s.Allocs, s.AllocBytes, s.PeakBytes, s.AtPeak
			}
		}
		t.Fatalf("no site for line %d in %s", line, raw)
		return
	}

	// label allocates in label on every call, 2000 + 5000 of them. The
	// interpolation makes the number's string and then the result, so the
	// count is a whole multiple of the calls rather than exactly one each.
	if a, _, _, _ := site(profLineLabel); a == 0 || a%7000 != 0 {
		t.Errorf("label line: %d allocations, want a multiple of 7000", a)
	}
	// The concatenation runs after label returns, so it is the caller's line
	// that pays for it — the frame restore at work.
	if a, _, _, _ := site(profLineConcat); a != 2000 {
		t.Errorf("concatenation line: %d allocations, want 2000", a)
	}
	// The array and map are allocated once each, and hold their grown
	// buffers: at the peak the array holds at least 2000 pointers and the
	// map more than 2000 entries.
	if a, _, peak, atPeak := site(profLineNames); a != 1 || peak < 2000*8 || atPeak < 2000*8 {
		t.Errorf("names line: %d allocations, peak %d, at peak %d", a, peak, atPeak)
	}
	if a, _, peak, _ := site(profLineIndex); a != 1 || peak < 2000*16 {
		t.Errorf("index line: %d allocations, peak %d", a, peak)
	}
	// Growing them is allocation by the line that pushes, not by the line
	// that declared them.
	if _, bytes, _, _ := site(profLinePush); bytes < 2000*8 {
		t.Errorf("push line: %d bytes of growth, want at least %d", bytes, 2000*8)
	}
	if _, bytes, _, _ := site(profLineSet); bytes < 2000*16 {
		t.Errorf("set line: %d bytes of growth, want at least %d", bytes, 2000*16)
	}
	if len(p.Timeline) < 2 {
		t.Errorf("expected a timeline, got %d points", len(p.Timeline))
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// The HTTP server replaces the profiler's SIGINT handler with its own and ends
// the process with _exit, which skips the atexit handler the profile is
// written from. Its shutdown path has to write the profile itself, or a
// profiled server stopped with Ctrl-C leaves nothing behind.
func TestProfileWrittenBeforeServerExit(t *testing.T) {
	const server = `
import "http"

fn ok(req: http.HttpRequest): http.HttpResponse {
    return http.response(200, "ok", "text/plain")
}

fn main(): void {
    http.route("GET", "/", ok)
    http.listen(8080)
}
`
	out := generateWith(t, server, withProfile)
	assertContains(t, out, "#define DEX_PF_FLUSH() dex_pf_shutdown()")
	if !strings.Contains(out, "DEX_PF_FLUSH();\n    _exit(0);") {
		t.Errorf("the HTTP shutdown handler must write the profile before _exit")
	}
	if strings.Count(out, "_exit(0);") > strings.Count(out, "DEX_PF_FLUSH();")+1 {
		// One _exit is a forked worker's, which must not write over the parent's profile.
		t.Errorf("an _exit path skips the profile: %d exits, %d flushes",
			strings.Count(out, "_exit(0);"), strings.Count(out, "DEX_PF_FLUSH();"))
	}
}
