package faultinject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type outcome string

const (
	clean       outcome = "clean"
	leaked      outcome = "leak"
	uncaught    outcome = "uncaught"
	failed      outcome = "failed"
	memoryError outcome = "memory-error"
	crashed     outcome = "crash"
	hung        outcome = "timeout"
)

func fatal(o outcome) bool {
	return o == memoryError || o == crashed || o == hung
}

type result struct {
	n       int
	outcome outcome
	site    string
	detail  string
}

var (
	countRe    = regexp.MustCompile(`fault injection: (\d+) allocations`)
	injectedRe = regexp.MustCompile(`injected allocation failure #\d+ at (\S+)`)
	memErrRe   = regexp.MustCompile(`memory error: (.*)\n\s+while at\s+(\S+)`)
	leakSiteRe = regexp.MustCompile(`allocated at (\S+)`)
)

var skipped = map[string]string{
	"httpThreads_test.dx":    "serves forever",
	"httpRequest_test.dx":    "needs the network",
	"httpHeaders.dx":         "serves forever",
	"httpNotFound_test.dx":   "serves forever",
	"httpOptions_test.dx":    "serves forever",
	"routeParams_test.dx":    "serves forever",
	"kafka_test.dx":          "needs a broker",
	"redis_test.dx":          "needs a server",
	"ws_test.dx":             "serves forever",
	"wsHeader_test.dx":       "serves forever",
	"externalSqlite_test.dx": "needs sqlite headers",
}

func repoRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd)
}

func buildCompiler(t *testing.T, root, dir string) string {
	dex := filepath.Join(dir, "dex")
	cmd := exec.Command("go", "build", "-o", dex, ".")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the compiler: %v\n%s", err, out)
	}
	return dex
}

func buildProgram(dex, src, dir string) (string, error) {
	cmd := exec.Command(dex, "build", "--no-lint", src)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DEX_MEM_CHECK=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v\n%s", err, out)
	}
	return filepath.Join(dir, "build", strings.TrimSuffix(filepath.Base(src), ".dx")), nil
}

func run(bin, dir string, env []string, limit time.Duration) (string, int, error, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	return stderr.String(), code, err, ctx.Err() != nil
}

func scopeEnv() string {
	if os.Getenv("DEX_FAULT_SCOPE") == "all" {
		return "DEX_FAIL_ALLOC_SCOPE=all"
	}
	return "DEX_FAIL_ALLOC_SCOPE=try"
}

func allocationCount(bin, dir string) (int, error) {
	stderr, code, _, timedOut := run(bin, dir, []string{"DEX_FAIL_ALLOC_REPORT=1", scopeEnv()}, 20*time.Second)
	if timedOut {
		return 0, errors.New("timed out without injection")
	}
	if code != 0 {
		return 0, fmt.Errorf("exit %d without injection\n%s", code, stderr)
	}
	m := countRe.FindStringSubmatch(stderr)
	if m == nil {
		return 0, nil
	}
	return strconv.Atoi(m[1])
}

func classify(stderr string, code int, err error, timedOut bool) (outcome, string, string) {
	site := ""
	if m := injectedRe.FindStringSubmatch(stderr); m != nil {
		site = shortSite(m[1])
	}
	if timedOut {
		return hung, site, ""
	}
	if m := memErrRe.FindStringSubmatch(stderr); m != nil {
		return memoryError, site, m[1] + " at " + shortSite(m[2])
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && code < 0 {
		return crashed, site, exitErr.String()
	}
	if strings.Contains(stderr, "Unhandled Exception: injected allocation failure") {
		return uncaught, site, ""
	}
	if code != 0 {
		return failed, site, firstLine(stderr)
	}
	if strings.Contains(stderr, "still alive at exit") {
		sites := leakSiteRe.FindAllStringSubmatch(stderr, -1)
		var names []string
		for _, s := range sites {
			names = append(names, shortSite(s[1]))
		}
		sort.Strings(names)
		return leaked, site, strings.Join(names, ", ")
	}
	return clean, site, ""
}

func shortSite(s string) string {
	return filepath.Base(s)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func points(count, limit int) []int {
	if count <= limit {
		ns := make([]int, count)
		for i := range ns {
			ns[i] = i + 1
		}
		return ns
	}
	ns := make([]int, 0, limit)
	for i := 0; i < limit; i++ {
		ns = append(ns, 1+i*(count-1)/(limit-1))
	}
	return ns
}

func sweep(t *testing.T, dex, src string, limit int) []result {
	work := t.TempDir()
	bin, err := buildProgram(dex, src, work)
	if err != nil {
		if strings.Contains(src, filepath.Join("faultinject", "testdata")) {
			t.Errorf("%s: build failed: %v", filepath.Base(src), err)
		} else {
			t.Logf("%s: build failed, skipped: %v", filepath.Base(src), err)
		}
		return nil
	}
	count, err := allocationCount(bin, work)
	if err != nil {
		t.Logf("%s: skipped: %v", filepath.Base(src), err)
		return nil
	}
	ns := points(count, limit)
	results := make([]result, len(ns))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		dir := filepath.Join(work, "w"+strconv.Itoa(w))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				n := ns[i]
				stderr, code, err, timedOut := run(bin, dir, []string{
					"DEX_FAIL_ALLOC=" + strconv.Itoa(n),
					"DEX_MEM_LEAKS=report",
					scopeEnv(),
				}, 20*time.Second)
				o, site, detail := classify(stderr, code, err, timedOut)
				results[i] = result{n: n, outcome: o, site: site, detail: detail}
			}
		}()
	}
	for i := range ns {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func report(t *testing.T, name string, results []result) (map[outcome]int, int) {
	tally := map[outcome]int{}
	type key struct {
		o      outcome
		site   string
		detail string
	}
	groups := map[key][]int{}
	bad := 0
	for _, r := range results {
		tally[r.outcome]++
		if fatal(r.outcome) || r.outcome == leaked {
			k := key{r.outcome, r.site, r.detail}
			groups[k] = append(groups[k], r.n)
		}
		if fatal(r.outcome) {
			bad++
		}
	}
	var keys []key
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].o != keys[j].o {
			return keys[i].o < keys[j].o
		}
		return keys[i].site < keys[j].site
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d runs", name, len(results))
	for _, o := range []outcome{clean, leaked, uncaught, failed, memoryError, crashed, hung} {
		if tally[o] > 0 {
			fmt.Fprintf(&b, ", %s %d", o, tally[o])
		}
	}
	for _, k := range keys {
		ns := groups[k]
		fmt.Fprintf(&b, "\n  %s x%d (N=%d", k.o, len(ns), ns[0])
		if len(ns) > 1 {
			fmt.Fprintf(&b, "..%d", ns[len(ns)-1])
		}
		fmt.Fprintf(&b, ") injected at %s", k.site)
		if k.detail != "" {
			fmt.Fprintf(&b, ": %s", k.detail)
		}
	}
	t.Log(b.String())
	return tally, bad
}

func sources(t *testing.T, root string) []string {
	fixtures, _ := filepath.Glob(filepath.Join(root, "faultinject", "testdata", "*.dx"))
	if os.Getenv("DEX_FAULT") != "1" {
		return fixtures
	}
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*_test.dx"))
	var out []string
	for _, e := range examples {
		if _, skip := skipped[filepath.Base(e)]; !skip {
			out = append(out, e)
		}
	}
	out = append(out, fixtures...)
	if filter := os.Getenv("DEX_FAULT_ONLY"); filter != "" {
		var kept []string
		for _, s := range out {
			if strings.Contains(filepath.Base(s), filter) {
				kept = append(kept, s)
			}
		}
		out = kept
	}
	return out
}

func TestFaultInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs every program once per allocation")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler")
	}
	root := repoRoot(t)
	dex := buildCompiler(t, root, t.TempDir())
	limit := envInt("DEX_FAULT_MAX", 300)
	if os.Getenv("DEX_FAULT") != "1" {
		limit = envInt("DEX_FAULT_MAX", 60)
	}
	totals := map[outcome]int{}
	programs, runs, bad := 0, 0, 0
	for _, src := range sources(t, root) {
		results := sweep(t, dex, src, limit)
		if results == nil {
			continue
		}
		tally, b := report(t, filepath.Base(src), results)
		for o, c := range tally {
			totals[o] += c
		}
		programs++
		runs += len(results)
		bad += b
	}
	t.Logf("%d programs, %d runs: %v", programs, runs, totals)
	if bad > 0 {
		t.Errorf("%d runs ended in a memory error, a crash or a hang", bad)
	}
}
