// Package lint reports what compiles but is probably not what was meant: a
// binding nothing reads, a statement nothing reaches, an exception nothing
// handles. None of it blocks a build by itself — the compiler accepts every
// program lint complains about — so each finding carries the name of the rule
// that produced it, and every rule can be turned off either on the command line
// or with an #[allow(...)] on the declaration it fires on.
//
// Lint runs after the checker, on the checked AST, and depends on that: a
// method call only knows which struct it belongs to once the checker has filled
// in CallExpr.StructType.
package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// Rule names. These are the strings a user types in --lint-off and #[allow(...)],
// so they are part of the interface and do not change silently.
const (
	RuleUnusedVar        = "unused-var"
	RuleUnusedParam      = "unused-param"
	RuleUnusedImport     = "unused-import"
	RuleUnusedFunc       = "unused-function"
	RuleShadow           = "shadow"
	RuleUnreachable      = "unreachable"
	RuleUncaughtThrow    = "uncaught-throw"
	RuleUndeclaredThrow  = "undeclared-throw"
	RuleUnneededThrows   = "unnecessary-throws"
	RuleUnreachableCatch = "unreachable-catch"
	RuleUnknownAllow     = "unknown-allow"
	RuleStructPadding    = "struct-padding"
)

// AllRules lists every rule, in the order the help text shows them.
var AllRules = []string{
	RuleUnusedVar,
	RuleUnusedParam,
	RuleUnusedImport,
	RuleUnusedFunc,
	RuleShadow,
	RuleUnreachable,
	RuleUncaughtThrow,
	RuleUndeclaredThrow,
	RuleUnneededThrows,
	RuleUnreachableCatch,
	RuleUnknownAllow,
	RuleStructPadding,
}

// ruleDoc is the one-line description `dex lint --rules` prints.
var ruleDoc = map[string]string{
	RuleUnusedVar:        "a local or loop variable that is never read",
	RuleUnusedParam:      "a parameter that the function body never reads",
	RuleUnusedImport:     "an imported module that is never referenced",
	RuleUnusedFunc:       "a private function that nothing calls",
	RuleShadow:           "a declaration that hides a name from an enclosing scope",
	RuleUnreachable:      "a statement after a return, throw, break or continue",
	RuleUncaughtThrow:    "an exception that escapes a function not declared throws",
	RuleUndeclaredThrow:  "an exception type a throws declaration leaves out",
	RuleUnneededThrows:   "a throws declaration naming a type the function cannot throw",
	RuleUnreachableCatch: "a catch clause that can never run",
	RuleUnknownAllow:     "an #[allow(...)] naming a rule that does not exist",
	RuleStructPadding:    "a struct whose field order wastes bytes on padding",
}

// RuleDoc returns the one-line description of a rule.
func RuleDoc(rule string) string { return ruleDoc[rule] }

// A Finding is one diagnostic: where, which rule, and what to do about it.
type Finding struct {
	Pos  ast.Pos
	Rule string
	Msg  string
}

// Error formats the finding the way the checker formats its own diagnostics —
// file:line:col: message — so the CLI's colouring and the LSP's position parser
// both already understand it. The rule name is appended so it can be silenced.
func (f Finding) Error() string {
	if f.Pos.Line > 0 {
		if f.Pos.File != "" {
			return fmt.Sprintf("%s:%d:%d: %s [%s]", f.Pos.File, f.Pos.Line, f.Pos.Col, f.Msg, f.Rule)
		}
		return fmt.Sprintf("%d:%d: %s [%s]", f.Pos.Line, f.Pos.Col, f.Msg, f.Rule)
	}
	return fmt.Sprintf("%s [%s]", f.Msg, f.Rule)
}

// Options selects which rules run.
type Options struct {
	// Disable turns individual rules off by name.
	Disable map[string]bool
}

// Run analyses a checked program and returns its findings, ordered by position
// so output is stable across runs.
func Run(program *ast.Program, opts Options) []Finding {
	l := &linter{program: program, opts: opts, seen: map[ast.Expr]bool{}}

	l.checkAllowAnnotations()
	l.checkScopes()
	l.checkUnreachable()
	l.checkImports()
	l.checkPrivateFunctions()
	l.checkThrows()
	l.checkStructPadding()

	sort.SliceStable(l.findings, func(i, j int) bool {
		a, b := l.findings[i].Pos, l.findings[j].Pos
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return l.findings[i].Rule < l.findings[j].Rule
	})
	return l.findings
}

type linter struct {
	program  *ast.Program
	opts     Options
	findings []Finding
	// seen guards against analysing one lambda or spawn body twice. The shared
	// expression walker reports a nested lambda to every enclosing level, so
	// without this a lambda inside a lambda is examined once per level.
	seen map[ast.Expr]bool

	// Whole-program indexes, each built once on first use.
	identCache  map[string]bool
	callCache   map[string]bool
	moduleCache map[string]bool
}

func (l *linter) report(rule string, pos ast.Pos, format string, args ...interface{}) {
	if l.opts.Disable[rule] {
		return
	}
	l.findings = append(l.findings, Finding{Pos: pos, Rule: rule, Msg: fmt.Sprintf(format, args...)})
}

// allows reports whether an #[allow(...)] on a declaration covers this rule.
// `allow(all)` covers everything and `allow(unused)` covers the unused-* family,
// which is what someone writing it almost always means.
func allows(annotations []string, rule string) bool {
	for _, a := range annotations {
		named, ok := ast.ParseAllowAnnotation(a)
		if !ok {
			continue
		}
		switch named {
		case rule, allowAll:
			return true
		case allowUnused:
			switch rule {
			case RuleUnusedVar, RuleUnusedParam, RuleUnusedImport, RuleUnusedFunc:
				return true
			}
		}
	}
	return false
}

// The two aliases #[allow(...)] accepts besides a rule name.
const (
	allowAll    = "all"
	allowUnused = "unused"
)

// knownAllowTarget reports whether a name inside #[allow(...)] means anything.
func knownAllowTarget(name string) bool {
	if name == allowAll || name == allowUnused {
		return true
	}
	return ruleDoc[name] != ""
}

// splitMethodName takes the flat name a struct method was merged under and
// recovers the two halves: Box_risky is Box's risky. The prefix has to name a
// registered struct, or an ordinary function with an underscore in it would be
// mistaken for a method.
func splitMethodName(flat string) (structName, method string, ok bool) {
	for i := 0; i < len(flat); i++ {
		if flat[i] != '_' {
			continue
		}
		head := flat[:i]
		if _, found := ast.LookupStructType(head); found {
			return head, flat[i+1:], true
		}
	}
	return "", "", false
}

// childBlocks returns the statement lists a statement owns directly. A
// statement kind this does not know about contributes nothing, which costs a
// missed finding rather than a wrong one.
func childBlocks(stmt ast.Stmt) [][]ast.Stmt {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		return [][]ast.Stmt{s.Then, s.Else}
	case *ast.WhileStmt:
		return [][]ast.Stmt{s.Body}
	case *ast.ForStmt:
		return [][]ast.Stmt{s.Body}
	case *ast.ForeachStmt:
		return [][]ast.Stmt{s.Body}
	case *ast.BlockStmt:
		return [][]ast.Stmt{s.Stmts}
	case *ast.TryCatchStmt:
		blocks := make([][]ast.Stmt, 0, len(s.Catches)+2)
		blocks = append(blocks, s.Body)
		for i := range s.Catches {
			blocks = append(blocks, s.Catches[i].Body)
		}
		return append(blocks, s.FinallyBody)
	case *ast.SwitchStmt:
		blocks := make([][]ast.Stmt, 0, len(s.Cases)+1)
		for i := range s.Cases {
			blocks = append(blocks, s.Cases[i].Body)
		}
		return append(blocks, s.Default)
	}
	return nil
}

func (l *linter) checkStructPadding() {
	seen := map[ast.Pos]bool{}
	for i := range l.program.Structs {
		sd := &l.program.Structs[i]
		if sd.Pos.Line == 0 || seen[sd.Pos] || allows(sd.Annotations, RuleStructPadding) {
			continue
		}
		seen[sd.Pos] = true
		layout := ast.LayoutFields(sd.Fields)
		if layout.Packed >= layout.Size {
			continue
		}
		l.report(RuleStructPadding, sd.Pos,
			"struct '%s' is %d bytes as declared and %d with its fields ordered by alignment: %s",
			ast.DisplayStructName(sd.Name), layout.Size, layout.Packed, strings.Join(layout.Order, ", "))
	}
}
