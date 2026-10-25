package lint

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// lintSource runs the pipeline a real build runs — lex, parse, flatten methods,
// check — and then lints. Going through the checker matters: a method call only
// knows its struct once the checker has resolved it, and the throw analysis
// reads that.
func lintSource(t *testing.T, source string, opts Options) []Finding {
	t.Helper()
	ast.ResetStructTypes()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()
	ast.ResetWeakTypes()
	ast.ResetStructArrayTypes()
	ast.ResetOptionalTypes()
	ast.ResetRefTypes()
	ast.ResetFuncTypes()
	ast.ResetMapTypes()
	ast.ResetEnumTypes()
	ast.ResetInterfaceTypes()
	stdlib.RegisterAllModuleTypes()
	ast.RegisterExceptionType()

	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	var importPaths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			importPaths = append(importPaths, tokens[i+1].Value)
		}
	}

	p := parser.New(tokens)
	for _, name := range stdlib.ModuleTypesForImports(importPaths) {
		p.AddStructName(name)
	}
	p.AddStructName("Exception")
	program, parseErrs := p.Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("parser error: %v", parseErrs[0])
	}
	resolve.FlattenStructMethods(program)
	if errs := checker.New().Check(program); len(errs) > 0 {
		t.Fatalf("checker error: %v", errs[0])
	}
	return Run(program, opts)
}

// rules returns the rule of every finding, so a test can assert on what was
// reported without pinning the wording.
func rules(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Rule)
	}
	return out
}

func assertRules(t *testing.T, source string, want ...string) []Finding {
	t.Helper()
	findings := lintSource(t, source, Options{})
	got := rules(findings)
	if len(got) != len(want) {
		t.Fatalf("got %d findings %v, want %d %v\n%s", len(got), got, len(want), want, detail(findings))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("finding %d is %s, want %s\n%s", i, got[i], want[i], detail(findings))
		}
	}
	return findings
}

func detail(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("  " + f.Error() + "\n")
	}
	return b.String()
}

func TestCleanProgramHasNoFindings(t *testing.T) {
	assertRules(t, `
fn add(a: int, b: int): int {
    return a + b
}

fn main(): void {
    let total = add(1, 2)
    assert(total == 3)
}
`)
}

func TestUnusedVariable(t *testing.T) {
	findings := assertRules(t, `
fn main(): void {
    let unread = 7
    assert(true)
}
`, RuleUnusedVar)
	if !strings.Contains(findings[0].Msg, "'unread'") {
		t.Errorf("message does not name the variable: %s", findings[0].Msg)
	}
}

func TestWrittenButNeverReadIsUnused(t *testing.T) {
	// Assigning is not using: a variable only ever written to is dead.
	assertRules(t, `
fn main(): void {
    let x = 1
    x = 2
    assert(true)
}
`, RuleUnusedVar)
}

func TestReadInNestedBlockIsUsed(t *testing.T) {
	assertRules(t, `
fn main(): void {
    let x = 1
    if (true) {
        assert(x == 1)
    }
}
`)
}

func TestReadInLambdaCountsAsUsed(t *testing.T) {
	// A captured variable is read when the lambda runs, which still counts.
	assertRules(t, `
fn main(): void {
    let n = 2
    let scale: fn(int): int = fn(x: int): int { return x * n }
    assert(scale(4) == 8)
}
`)
}

func TestCallingAFunctionValueIsAUse(t *testing.T) {
	// `f(1)` reads f even though it is spelled like a call to a top-level
	// function rather than as a bare name.
	assertRules(t, `
fn twice(x: int): int { return x * 2 }

fn main(): void {
    let f: fn(int): int = twice
    assert(f(2) == 4)
}
`)
}

func TestUnusedLambdaLocal(t *testing.T) {
	// A declaration inside a lambda body is in scope too.
	assertRules(t, `
fn main(): void {
    let f: fn(int): int = fn(x: int): int {
        let unused = 9
        return x
    }
    assert(f(1) == 1)
}
`, RuleUnusedVar)
}

func TestUnusedLoopVariable(t *testing.T) {
	assertRules(t, `
fn main(): void {
    let nums: int[] = [1, 2, 3]
    let count = 0
    foreach(nums as i, n) {
        count += i
    }
    assert(count == 3)
}
`, RuleUnusedVar)
}

func TestUnusedParameter(t *testing.T) {
	assertRules(t, `
fn pick(a: int, b: int): int {
    return a
}

fn main(): void {
    assert(pick(1, 2) == 1)
}
`, RuleUnusedParam)
}

func TestParameterOfAFunctionUsedAsValueIsNotReported(t *testing.T) {
	// The signature belongs to whatever receives the function, so an ignored
	// parameter there is a contract rather than an oversight.
	assertRules(t, `
fn handler(a: int, b: int): int {
    return a
}

fn apply(f: fn(int, int): int): int {
    return f(1, 2)
}

fn main(): void {
    assert(apply(handler) == 1)
}
`)
}

func TestSelfIsNeverReportedUnused(t *testing.T) {
	assertRules(t, `
struct Box {
    n: int

    fn constant(): int {
        return 7
    }
}

fn main(): void {
    let b = Box { n: 1 }
    assert(b.constant() == 7)
}
`)
}

func TestShadowedDeclaration(t *testing.T) {
	findings := assertRules(t, `
fn main(): void {
    let x = 1
    if (true) {
        let x = 2
        assert(x == 2)
    }
    assert(x == 1)
}
`, RuleShadow)
	if !strings.Contains(findings[0].Msg, "hides") {
		t.Errorf("message does not say what it hides: %s", findings[0].Msg)
	}
}

func TestSiblingScopesDoNotShadow(t *testing.T) {
	assertRules(t, `
fn main(): void {
    if (true) {
        let x = 1
        assert(x == 1)
    }
    if (true) {
        let x = 2
        assert(x == 2)
    }
}
`)
}

func TestUnreachableAfterReturn(t *testing.T) {
	assertRules(t, `
fn f(n: int): int {
    if (n > 0) {
        return 1
        assert(false)
    }
    return 0
}

fn main(): void {
    assert(f(1) == 1)
}
`, RuleUnreachable)
}

func TestUnusedPrivateFunction(t *testing.T) {
	assertRules(t, `
private fn helper(): int {
    return 1
}

fn main(): void {
    assert(true)
}
`, RuleUnusedFunc)
}

func TestPrivateFunctionPassedAsValueIsUsed(t *testing.T) {
	assertRules(t, `
private fn helper(x: int): int {
    return x
}

fn apply(f: fn(int): int): int {
    return f(3)
}

fn main(): void {
    assert(apply(helper) == 3)
}
`)
}

func TestUncaughtThrowAtTheThrow(t *testing.T) {
	findings := assertRules(t, `
fn risky(): int {
    throw Exception("no")
}

fn main(): void {
    try {
        assert(risky() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, RuleUncaughtThrow)
	if !strings.Contains(findings[0].Msg, "declare the signature throws") {
		t.Errorf("message does not offer the declaration: %s", findings[0].Msg)
	}
}

func TestDeclaredThrowsSilencesItsOwnThrow(t *testing.T) {
	assertRules(t, `
fn risky(): int throws {
    throw Exception("no")
}

fn main(): void {
    try {
        assert(risky() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`)
}

func TestThrowPropagatesToAnUnguardedCaller(t *testing.T) {
	// parse declares it; wrapper does not, so wrapper is the finding.
	findings := assertRules(t, `
fn parse(): int throws {
    throw Exception("no")
}

fn wrapper(): int {
    return parse()
}

fn main(): void {
    try {
        assert(wrapper() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, RuleUncaughtThrow)
	if !strings.Contains(findings[0].Msg, "'parse' may throw") {
		t.Errorf("message does not name the callee: %s", findings[0].Msg)
	}
}

func TestTryWithCatchGuardsTheCall(t *testing.T) {
	assertRules(t, `
fn parse(): int throws {
    throw Exception("no")
}

fn wrapper(): int {
    try {
        return parse()
    } catch (e: Exception) {
        return 0
    }
}

fn main(): void {
    assert(wrapper() == 0)
}
`)
}

func TestTryWithOnlyFinallyDoesNotGuard(t *testing.T) {
	// The finally runs and the exception carries on, so wrapper still leaks it.
	assertRules(t, `
fn parse(): int throws {
    throw Exception("no")
}

fn wrapper(): int {
    try {
        return parse()
    } finally {
        assert(true)
    }
}

fn main(): void {
    try {
        assert(wrapper() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, RuleUncaughtThrow)
}

func TestThrowInsideALambdaDoesNotEscapeTheEnclosingFunction(t *testing.T) {
	// It escapes whoever invokes the lambda, which is not knowable here.
	assertRules(t, `
fn main(): void {
    let f: fn(int): int = fn(x: int): int {
        throw Exception("no")
    }
    try {
        assert(f(1) == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, RuleUnusedParam)
}

func TestMainIsToldToCatchRatherThanDeclare(t *testing.T) {
	findings := assertRules(t, `
fn risky(): int throws {
    throw Exception("no")
}

fn main(): void {
    assert(risky() == 0)
}
`, RuleUncaughtThrow)
	if !strings.Contains(findings[0].Msg, "nothing above main") {
		t.Errorf("main should be told to catch it: %s", findings[0].Msg)
	}
}

func TestUnnecessaryThrows(t *testing.T) {
	assertRules(t, `
fn safe(): int throws {
    return 1
}

fn main(): void {
    assert(safe() == 1)
}
`, RuleUnneededThrows, RuleUncaughtThrow)
}

func TestRecursiveThrowTerminates(t *testing.T) {
	// The propagation is a fixpoint, so a cycle must not spin.
	findings := lintSource(t, `
fn a(n: int): int throws {
    if (n > 0) {
        return b(n - 1)
    }
    throw Exception("done")
}

fn b(n: int): int throws {
    return a(n)
}

fn main(): void {
    try {
        assert(a(2) == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, Options{})
	for _, f := range findings {
		if f.Rule == RuleUnneededThrows {
			t.Errorf("mutual recursion should still count as throwing: %s", f.Error())
		}
	}
}

func TestMethodThrowIsNamedForAReader(t *testing.T) {
	findings := assertRules(t, `
struct Box {
    n: int

    fn risky(): int throws {
        throw Exception("no")
    }
}

fn main(): void {
    let b = Box { n: 1 }
    assert(b.risky() == 0)
}
`, RuleUncaughtThrow)
	if !strings.Contains(findings[0].Msg, "Box.risky") {
		t.Errorf("a method should be named Struct.method: %s", findings[0].Msg)
	}
}

func TestUnusedImport(t *testing.T) {
	findings := assertRules(t, `
import "str"

fn main(): void {
    assert(true)
}
`, RuleUnusedImport)
	if findings[0].Pos.Line != 2 {
		t.Errorf("import finding should point at the import line, got %d", findings[0].Pos.Line)
	}
}

func TestUsedImportIsNotReported(t *testing.T) {
	assertRules(t, `
import "str"

fn main(): void {
    assert(str.fromInt(1) == "1")
}
`)
}

func TestAllowAnnotationSuppresses(t *testing.T) {
	assertRules(t, `
fn main(): void {
    #[allow(unused)]
    let unread = 7
    assert(true)
}
`)
}

func TestAllowAllSuppressesEverything(t *testing.T) {
	assertRules(t, `
#[allow(all)]
private fn helper(): int {
    return 1
}

fn main(): void {
    assert(true)
}
`)
}

func TestDisableOptionSuppressesARule(t *testing.T) {
	findings := lintSource(t, `
fn main(): void {
    let unread = 7
    assert(true)
}
`, Options{Disable: map[string]bool{RuleUnusedVar: true}})
	if len(findings) != 0 {
		t.Fatalf("expected the rule to be off, got %v", rules(findings))
	}
}

func TestFindingsAreOrderedByPosition(t *testing.T) {
	findings := lintSource(t, `
fn main(): void {
    let first = 1
    let second = 2
    let third = 3
    assert(true)
}
`, Options{})
	if len(findings) != 3 {
		t.Fatalf("expected three findings, got %v", rules(findings))
	}
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Pos.Line > findings[i].Pos.Line {
			t.Errorf("findings out of order: %d after %d", findings[i].Pos.Line, findings[i-1].Pos.Line)
		}
	}
}

func TestEveryRuleIsDocumented(t *testing.T) {
	for _, rule := range AllRules {
		if RuleDoc(rule) == "" {
			t.Errorf("rule %s has no description, so --rules cannot list it", rule)
		}
	}
}

func TestUnknownAllowTargetIsReported(t *testing.T) {
	// A suppression that silences nothing reads as handled and behaves as
	// forgotten, so the typo has to be visible.
	findings := assertRules(t, `
fn main(): void {
    #[allow(unsued-var)]
    let unread = 7
    assert(true)
}
`, RuleUnknownAllow, RuleUnusedVar)
	if !strings.Contains(findings[0].Msg, "unsued-var") {
		t.Errorf("message does not quote the bad rule name: %s", findings[0].Msg)
	}
}

func TestAllowOnASpecificRuleLeavesOthersAlone(t *testing.T) {
	assertRules(t, `
#[allow(unnecessary-throws)]
fn safe(): int throws {
    return 1
}

fn main(): void {
    assert(safe() == 1)
}
`, RuleUncaughtThrow)
}

func TestAllowOnAMethodSilencesIt(t *testing.T) {
	assertRules(t, `
struct Columns {
    fn index(field: string): int {
        return field.len()
    }

    #[allow(uncaught-throw)]
    fn require(field: string): int {
        if (field == "") {
            throw Exception("no projected column")
        }
        return 1
    }
}

fn main(): void {
    let c = Columns {}
    assert(c.index("a") + c.require("a") == 2)
}
`)
}

func TestFindingFormatMatchesCompilerDiagnostics(t *testing.T) {
	// The CLI colours and the LSP locates findings by parsing this shape, so
	// it has to stay file:line:col: message [rule].
	f := Finding{
		Pos:  ast.Pos{File: "app.dx", Line: 12, Col: 5},
		Rule: RuleUnusedVar,
		Msg:  "variable 'x' is declared but never read",
	}
	if got, want := f.Error(), "app.dx:12:5: variable 'x' is declared but never read [unused-var]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A finding with nowhere to point still names its rule.
	bare := Finding{Rule: RuleUnusedImport, Msg: "import 'str' is never used"}
	if got, want := bare.Error(), "import 'str' is never used [unused-import]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnreachableInEveryKindOfBlock(t *testing.T) {
	// Each statement that owns a body has to be descended into; a kind left out
	// of childBlocks silently stops reporting.
	assertRules(t, `
fn f(n: int): int {
    while (n > 10) {
        return 1
        assert(false)
    }
    for (let i = 0; i < n; i = i + 1) {
        continue
        assert(false)
    }
    let nums: int[] = [1, 2]
    foreach(nums as v) {
        return v
        assert(false)
    }
    switch (n) {
        case 1: {
            return 1
            assert(false)
        }
        default: {
            return 0
            assert(false)
        }
    }
}

fn main(): void {
    assert(f(1) == 1)
}
`, RuleUnreachable, RuleUnreachable, RuleUnreachable, RuleUnreachable, RuleUnreachable)
}

func TestTypedCatchGuardsOnlyItsOwnType(t *testing.T) {
	// The ParseError is handled; the IOError the same call can raise is not.
	findings := assertRules(t, `
struct ParseError(message: string, line: int) {}
struct IOError(message: string) {}

fn load(path: string): int throws ParseError, IOError {
    if (path == "") {
        throw IOError("no path")
    }
    throw ParseError("bad digit", 12)
}

fn main(): void {
    try {
        assert(load("x") == 0)
    } catch (e: ParseError) {
        assert(true)
    }
}
`, RuleUncaughtThrow)
	if !strings.Contains(findings[0].Msg, "IOError") {
		t.Errorf("the unguarded type should be named: %s", findings[0].Msg)
	}
	if strings.Contains(findings[0].Msg, "ParseError") {
		t.Errorf("the guarded type should not be reported: %s", findings[0].Msg)
	}
}

func TestEveryTypeCaughtIsClean(t *testing.T) {
	assertRules(t, `
struct ParseError(message: string, line: int) {}
struct IOError(message: string) {}

fn load(path: string): int throws ParseError, IOError {
    if (path == "") {
        throw IOError("no path")
    }
    throw ParseError("bad digit", 12)
}

fn main(): void {
    try {
        assert(load("x") == 0)
    } catch (e: ParseError) {
        assert(e.line == 12)
    } catch (e: IOError) {
        assert(true)
    }
}
`)
}

func TestThrowsDeclarationLeavingATypeOut(t *testing.T) {
	findings := assertRules(t, `
struct ParseError(message: string) {}
struct IOError(message: string) {}

fn load(): int throws ParseError, IOError {
    if (true) {
        throw IOError("no path")
    }
    throw ParseError("bad digit")
}

fn narrow(): int throws ParseError {
    return load()
}

fn main(): void {
    try {
        assert(narrow() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, RuleUndeclaredThrow)
	if !strings.Contains(findings[0].Msg, "add IOError") {
		t.Errorf("the message should say what to add: %s", findings[0].Msg)
	}
}

func TestCatchAllCatchesATypedThrow(t *testing.T) {
	// Exception is the catch-all, which is what every catch was before types.
	assertRules(t, `
struct ParseError(message: string) {}

fn load(): int throws ParseError {
    throw ParseError("bad digit")
}

fn main(): void {
    try {
        assert(load() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`)
}

func TestDeclaringATypeThatIsNeverThrown(t *testing.T) {
	findings := assertRules(t, `
struct ParseError(message: string) {}
struct IOError(message: string) {}

fn load(): int throws ParseError, IOError {
    throw ParseError("bad digit")
}

fn main(): void {
    try {
        assert(load() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`, RuleUnneededThrows)
	if !strings.Contains(findings[0].Msg, "IOError") {
		t.Errorf("the unused declared type should be named: %s", findings[0].Msg)
	}
}

func TestUnreachableCatchAfterCatchAll(t *testing.T) {
	assertRules(t, `
struct ParseError(message: string) {}

fn main(): void {
    try {
        assert(true)
    } catch (e: Exception) {
        assert(true)
    } catch (e: ParseError) {
        assert(true)
    }
}
`, RuleUnreachableCatch)
}

func TestDuplicateCatchClause(t *testing.T) {
	assertRules(t, `
struct ParseError(message: string) {}

fn main(): void {
    try {
        assert(true)
    } catch (e: ParseError) {
        assert(true)
    } catch (e: ParseError) {
        assert(true)
    }
}
`, RuleUnreachableCatch)
}

func TestBareThrowsStillCoversAnything(t *testing.T) {
	// A bare throws names nothing, so it promises nothing specific — and
	// therefore covers whatever the body raises.
	assertRules(t, `
struct ParseError(message: string) {}

fn load(): int throws {
    throw ParseError("bad digit")
}

fn main(): void {
    try {
        assert(load() == 0)
    } catch (e: Exception) {
        assert(true)
    }
}
`)
}

func TestBareThrowsIsNotCaughtByATypedClause(t *testing.T) {
	// What a bare throws raises is unknown, so only a catch-all can be sure of
	// stopping it.
	assertRules(t, `
struct ParseError(message: string) {}

fn load(): int throws {
    throw ParseError("bad digit")
}

fn main(): void {
    try {
        assert(load() == 0)
    } catch (e: ParseError) {
        assert(true)
    }
}
`, RuleUncaughtThrow)
}

func TestMethodKeepsItsThrowsTypeList(t *testing.T) {
	// A method is flattened into a plain function before lint sees it, and the
	// declared type list has to survive that: dropped, the method reads as a
	// bare throws and a matching catch stops guarding it.
	assertRules(t, `
struct ParseError(message: string, line: int) {}

struct Validator {
    min: int

    fn check(age: int): int throws ParseError {
        if (age < min) {
            throw ParseError("too young", age)
        }
        return age
    }
}

fn main(): void {
    let v = Validator { min: 18 }
    try {
        assert(v.check(9) == 0)
    } catch (e: ParseError) {
        assert(e.line == 9)
    }
}
`)
}

// An undeclared throw is reported once, where it is raised — not at every call
// site that can reach it. Inference is exact enough to report all of them, and
// on a real codebase one guard in a shared query builder reached 500 unguarded
// call sites: reporting each said nothing a reader could act on, because the
// decision about where to catch is made once.
func TestAnUndeclaredThrowIsReportedWhereItIsRaised(t *testing.T) {
	findings := assertRules(t, `
fn aliasFor(table: string): string {
    if (table == "") {
        throw Exception("a column named no table — the projection is wrong")
    }
    return table
}

fn columnsFor(table: string): string {
    return aliasFor(table)
}

fn main(): void {
    assert(columnsFor("t") == "t")
}
`, RuleUncaughtThrow)
	if findings[0].Pos.Line != 4 {
		t.Errorf("the finding belongs on the throw, got line %d", findings[0].Pos.Line)
	}
}

// Declaring it is what obligates the callers: that is the gradient Java gets
// from making the declaration mandatory, reached here by making the obligation
// follow the annotation.
func TestDeclaringAThrowObligatesTheCallers(t *testing.T) {
	findings := assertRules(t, `
fn aliasFor(table: string): string throws {
    if (table == "") {
        throw Exception("a column named no table — the projection is wrong")
    }
    return table
}

fn columnsFor(table: string): string {
    return aliasFor(table)
}

fn main(): void {
    assert(columnsFor("t") == "t")
}
`, RuleUncaughtThrow)
	// Reported at the call in columnsFor, not at the throw: aliasFor declared it.
	if !strings.Contains(findings[0].Msg, "'aliasFor' may throw") {
		t.Errorf("expected the call site to be named: %s", findings[0].Msg)
	}
	if !strings.Contains(findings[0].Msg, "'columnsFor' does not declare throws") {
		t.Errorf("expected the caller to be named: %s", findings[0].Msg)
	}
}

// And declaring it at each level moves the obligation outward, one step at a
// time, until something catches it.
func TestTheObligationMovesOutwardAsEachLevelDeclares(t *testing.T) {
	findings := assertRules(t, `
fn aliasFor(table: string): string throws {
    throw Exception("wrong " + table)
}

fn columnsFor(table: string): string throws {
    return aliasFor(table)
}

fn main(): void {
    let c = columnsFor("t")
    assert(c == "t")
}
`, RuleUncaughtThrow)
	if !strings.Contains(findings[0].Msg, "nothing above main") {
		t.Errorf("the obligation should have reached main: %s", findings[0].Msg)
	}
}

// Catching it ends the chain rather than moving it.
func TestCatchingItEndsTheChain(t *testing.T) {
	assertRules(t, `
fn aliasFor(table: string): string throws {
    throw Exception("wrong " + table)
}

fn columnsFor(table: string): string {
    try {
        return aliasFor(table)
    } catch (e: Exception) {
        return ""
    }
}

fn main(): void {
    assert(columnsFor("t") == "")
}
`)
}

func TestDisplayNameReadsModuleNames(t *testing.T) {
	ast.RegisterStructType(ast.StructDef{Name: "query_Columns", Module: "query"})
	l := &linter{program: &ast.Program{UserModules: []string{"query"}}}
	for flat, want := range map[string]string{
		"query_query_Columns_override": "query.Columns.override",
		"query_aliasFor":               "query.aliasFor",
		"query_Columns_index":          "query.Columns.index",
		"plain":                        "plain",
	} {
		if got := l.displayName(flat); got != want {
			t.Errorf("displayName(%q) = %q, want %q", flat, got, want)
		}
	}
}

func TestModuleMethodCallReachesItsFlatName(t *testing.T) {
	ast.ResetStructTypes()
	st := ast.RegisterStructType(ast.StructDef{Name: "sqlite_Statement", Module: "sqlite"})
	plain := ast.RegisterStructType(ast.StructDef{Name: "Box"})
	ast.RegisterStructType(ast.StructDef{Name: "sqlite_SqliteError", Module: "sqlite"})
	l := &linter{program: &ast.Program{UserModules: []string{"sqlite"}}}
	if got := l.calleeName(&ast.CallExpr{Name: "check", IsMethodCall: true, StructType: st}); got != "sqlite_sqlite_Statement_check" {
		t.Errorf("module method callee = %q", got)
	}
	if got := l.calleeName(&ast.CallExpr{Name: "risky", IsMethodCall: true, StructType: plain}); got != "Box_risky" {
		t.Errorf("main program method callee = %q", got)
	}
	if got := typeList([]string{"sqlite_SqliteError", "Exception"}); got != "sqlite.SqliteError, Exception" {
		t.Errorf("typeList = %q", got)
	}
}

func TestEnumConstructorArgumentsAreReads(t *testing.T) {
	assertRules(t, `
enum Shape {
    Label(string)
    Pair(int, int)
}

fn size(s: Shape): int {
    switch (s) {
        case Shape.Label(x): {
            return x.len()
        }
        case Shape.Pair(a, b): {
            return a + b
        }
    }
}

fn main(): void {
    let word: string = "w"
    let first: int = 1
    let second: int = 2
    assert(size(Shape.Label(word)) + size(Shape.Pair(first, second)) == 4)
}
`)
}

func TestStructPaddingReportsABetterOrder(t *testing.T) {
	findings := assertRules(t, `
struct Loose {
    on: bool
    total: long
    off: bool
}

#[allow(struct-padding)]
struct Kept {
    on: bool
    total: long
    off: bool
}

struct Tight {
    total: long
    on: bool
    off: bool
}

fn main(): void {
    let a = Loose { on: true, total: 1, off: false }
    let b = Kept { on: true, total: 1, off: false }
    let c = Tight { total: 1, on: true, off: false }
    assert(a.total + b.total + c.total == 3)
}
`, RuleStructPadding)
	if want := "struct 'Loose' would be 24 bytes in declared order; the compiler lays it out as total, on, off in 16"; findings[0].Msg != want {
		t.Errorf("got %q, want %q", findings[0].Msg, want)
	}
}
