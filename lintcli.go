package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lint"
	"golang.org/x/term"
)

const usage = `Usage: dex <command> [flags] <file.dx>

Commands:
  build    compile to a native binary
  run      compile and run
  dev      rebuild and rerun on change
  test     run *_test.dx files
  check    run cppcheck over the generated C
  format   lay files out the one canonical way
  mod      mod init <path>: start a dex.mod; mod download: fetch what it
           requires; mod verify: check the cache against dex.sum
  get      get <module>[@version]: add a module to dex.mod and download it
  lint     report what compiles but is probably not meant
  lsp      start the language server
  docs     regenerate docs/index.html
  update   update the dex binary
  version  print the version

Flags:
  --check          with format: list what is unformatted instead of rewriting it
  --no-lint        build without reporting lint findings (build, run and dev
                   report them by default)
  --lint           report lint findings with test, which leaves them off
  --lint=strict    report them and fail the build if there are any
  --lint-off=a,b   turn named rules off
  --rules          with lint: list the rules and what each one finds
  --force          with update: reinstall even if already current`

// The flags for this invocation. A command's behaviour is otherwise driven by
// environment variables (DEX_MEM_CHECK and friends); these are the options that
// belong on the command line because they change what is reported rather than
// what is emitted.
var (
	lintEnabled bool
	lintAsked   bool
	noLint      bool
	lintStrict  bool
	lintDisable = map[string]bool{}
	forceUpdate bool
	listRules   bool
	fmtCheck    bool
)

func lintOnByDefault(command string) bool {
	if lintAsked {
		return true
	}
	if noLint {
		return false
	}
	return command != "test"
}

// parseFlags removes the flags from args and returns the positional arguments.
// An unknown flag is an error rather than a filename: silently compiling the
// wrong thing because a flag was misspelled is worse than stopping.
func parseFlags(args []string) ([]string, error) {
	var rest []string
	for _, a := range args {
		switch {
		case a == "--lint":
			lintAsked = true
		case a == "--lint=strict":
			lintAsked, lintStrict = true, true
		case a == "--no-lint":
			noLint = true
		case strings.HasPrefix(a, "--lint-off="):
			for _, rule := range strings.Split(strings.TrimPrefix(a, "--lint-off="), ",") {
				rule = strings.TrimSpace(rule)
				if rule == "" {
					continue
				}
				if lint.RuleDoc(rule) == "" {
					return nil, fmt.Errorf("unknown lint rule '%s' (dex lint --rules lists them)", rule)
				}
				lintDisable[rule] = true
			}
		case a == "--check":
			fmtCheck = true
		case a == "--rules":
			listRules = true
		case a == "--force":
			forceUpdate = true
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("unknown flag '%s'\n\n%s", a, usage)
		default:
			rest = append(rest, a)
		}
	}
	return rest, nil
}

func printRules() {
	fmt.Println("Lint rules (turn one off with --lint-off=<rule>, or #[allow(<rule>)] on the declaration):")
	for _, rule := range lint.AllRules {
		fmt.Printf("  %-20s %s\n", rule, lint.RuleDoc(rule))
	}
	fmt.Println("\n#[allow(unused)] covers the four unused-* rules; #[allow(all)] covers everything.")
}

// lintOnly is the `dex lint` command: the front end and nothing else. No C is
// generated and no compiler is invoked, so it is the fast way to ask the
// question in an editor hook or a pre-commit check.
func lintOnly(filename string) {
	lintEnabled = true
	if _, err := frontend(filename); err != nil {
		fmt.Fprintln(os.Stderr, formatError(err.Error()))
		os.Exit(1)
	}
}

// reportLint prints the findings for a checked program. Findings never fail a
// build on their own — the program is valid — unless --lint=strict asked for
// that, which is what makes the flag usable in CI without blocking everyday
// work.
func reportLint(program *ast.Program) error {
	if !lintEnabled {
		return nil
	}
	findings := lint.Run(program, lint.Options{Disable: lintDisable})
	for _, f := range findings {
		fmt.Fprintln(os.Stderr, formatLint(f.Error()))
	}
	if len(findings) == 0 {
		return nil
	}
	if lintStrict {
		return fmt.Errorf("%s; --lint=strict treats them as errors", summarise(findings))
	}
	fmt.Fprintf(os.Stderr, "%s (--no-lint to build without them)\n", summarise(findings))
	return nil
}

// summarise counts the findings by rule. A bare total is no help once there are
// hundreds of them: one rule firing across a whole codebase and twenty rules
// firing once each are the same number and call for different reading.
func summarise(findings []lint.Finding) string {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Rule]++
	}
	byCount := make([]string, 0, len(counts))
	for _, rule := range lint.AllRules {
		if n := counts[rule]; n > 0 {
			byCount = append(byCount, fmt.Sprintf("%d %s", n, rule))
		}
	}
	sort.SliceStable(byCount, func(i, j int) bool {
		var a, b int
		fmt.Sscanf(byCount[i], "%d", &a)
		fmt.Sscanf(byCount[j], "%d", &b)
		return a > b
	})
	return plural(len(findings), "lint finding") + ": " + strings.Join(byCount, ", ")
}

func plural(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, what)
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// formatLint colours a finding the way formatError colours an error, in the
// yellow that says advisory rather than fatal.
func formatLint(msg string) string {
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return "lint: " + msg
	}
	if m := errorLocPattern.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("\033[1m%s\033[0m \033[33mlint:\033[0m %s", m[1], m[2])
	}
	return "\033[33mlint:\033[0m " + msg
}
