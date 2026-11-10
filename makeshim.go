package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// A stand-in for make, so gcc's link-time partitions run in parallel on a
// system without one. Reads only the makefile lto-wrapper writes; see
// csplit/README.md.

// isMakeShim reports whether this process was started as the make stand-in.
func isMakeShim() bool {
	return filepath.Base(os.Args[0]) == "make"
}

// miniMake runs the targets of a makefile in parallel, each target's recipe
// lines in order through /bin/sh.
func miniMake(args []string) int {
	file, jobs := "Makefile", 1
	var goals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version" || a == "-v":
			fmt.Println("GNU Make 4.3 compatible (dex, for gcc link-time optimisation only)")
			return 0
		case a == "-f" && i+1 < len(args):
			i++
			file = args[i]
		case a == "-j" && i+1 < len(args):
			i++
			jobs, _ = strconv.Atoi(args[i])
		case strings.HasPrefix(a, "-j"):
			jobs, _ = strconv.Atoi(a[2:])
		case strings.HasPrefix(a, "-"):
			// Flags that only change make's chatter.
		default:
			goals = append(goals, a)
		}
	}
	if jobs < 1 {
		jobs = 1
	}
	rules, err := readMakefile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dex make:", err)
		return 2
	}
	if len(goals) == 0 {
		goals = []string{"all"}
	}

	// Targets whose recipes run: the goals' prerequisites, then the goals.
	var order []string
	seen := map[string]bool{}
	var visit func(t string)
	visit = func(t string) {
		if seen[t] {
			return
		}
		seen[t] = true
		if r, ok := rules[t]; ok {
			for _, d := range r.deps {
				visit(d)
			}
		}
		order = append(order, t)
	}
	for _, g := range goals {
		visit(g)
	}

	// lto-wrapper's prerequisites do not depend on one another, so every
	// target but the goals can run at once.
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := false
	run := func(t string) {
		for _, line := range rules[t].recipe {
			ignore := false
			for len(line) > 0 && (line[0] == '@' || line[0] == '-' || line[0] == '+') {
				ignore = ignore || line[0] == '-'
				line = line[1:]
			}
			cmd := exec.Command("/bin/sh", "-c", line)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil && !ignore {
				mu.Lock()
				failed = true
				mu.Unlock()
				return
			}
		}
	}
	isGoal := map[string]bool{}
	for _, g := range goals {
		isGoal[g] = true
	}
	for _, t := range order {
		if isGoal[t] {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t string) {
			defer func() { <-sem; wg.Done() }()
			run(t)
		}(t)
	}
	wg.Wait()
	for _, t := range order {
		if isGoal[t] && !failed {
			run(t)
		}
	}
	if failed {
		return 2
	}
	return 0
}

type makeRule struct {
	deps   []string
	recipe []string
}

func readMakefile(path string) (map[string]*makeRule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rules := map[string]*makeRule{}
	var cur *makeRule
	var pending string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\t") && pending == "" {
			if cur != nil {
				cur.recipe = append(cur.recipe, line[1:])
			}
			continue
		}
		if strings.HasSuffix(line, "\\") {
			pending += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		line, pending = pending+line, ""
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf("%s: cannot read line %q", path, line)
		}
		cur = &makeRule{deps: strings.Fields(line[colon+1:])}
		for _, t := range strings.Fields(line[:colon]) {
			if t == ".PHONY" {
				cur = &makeRule{}
				continue
			}
			rules[t] = cur
		}
	}
	return rules, sc.Err()
}
