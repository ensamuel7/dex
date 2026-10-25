package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func format(t *testing.T, src string) string {
	t.Helper()
	out, err := Source(src, "test.dx")
	if err != nil {
		t.Fatalf("Source(%q): %v", src, err)
	}
	return out
}

func TestCanonicalShape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "indent and spacing are normalised",
			in:   "fn add(a:int,b:int):int{\n  return a+b\n}\n",
			want: "fn add(a: int, b: int): int {\n    return a + b\n}\n",
		},
		{
			name: "a statement broken for no reason is joined",
			in:   "fn f(): int {\n    return 1 +\n        2 +\n        3\n}\n",
			want: "fn f(): int {\n    return 1 + 2 + 3\n}\n",
		},
		{
			name: "a call too wide for the line breaks one argument per line",
			in: "fn f() {\n    send(aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, " +
				"cccccccccccccccccccccccccccccccc, dddddddddddddddddddddddd)\n}\n",
			want: "fn f() {\n" +
				"    send(\n" +
				"        aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,\n" +
				"        bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,\n" +
				"        cccccccccccccccccccccccccccccccc,\n" +
				"        dddddddddddddddddddddddd\n" +
				"    )\n}\n",
		},
		{
			// The comment used to end the statement's segment, so each half was
			// printed flat on its own: `P { a: 1,` / comment / `b: 2 }`.
			name: "a comment inside a literal keeps the literal expanded around it",
			in:   "fn f(): P {\n    return P {\n        a: 1, // first\n        // why b\n        b: 2\n    }\n}\n",
			want: "fn f(): P {\n    return P {\n        a: 1, // first\n        // why b\n        b: 2\n    }\n}\n",
		},
		{
			name: "a comment between call arguments keeps one argument per line",
			in:   "fn f() {\n    g(1, // one\n    2)\n}\n",
			want: "fn f() {\n    g(\n        1, // one\n        2\n    )\n}\n",
		},
		{
			name: "a comment in a lambda body inside a call is still a statement's",
			in:   "fn f() {\n    g(fn(): void {\n        // note\n        h()\n    })\n}\n",
			want: "fn f() {\n    g(fn(): void {\n        // note\n        h()\n    })\n}\n",
		},
		{
			name: "a struct literal is spaced, and expands when it has to",
			in:   "fn f(): Point {\n    return Point{x:1,y:2}\n}\n",
			want: "fn f(): Point {\n    return Point { x: 1, y: 2 }\n}\n",
		},
		{
			name: "runs of blank lines collapse to one",
			in:   "fn f() {\n    let a: int = 1\n\n\n\n    let b: int = 2\n}\n",
			want: "fn f() {\n    let a: int = 1\n\n    let b: int = 2\n}\n",
		},
		{
			name: "a blank line is added between declarations",
			in:   "fn a() {\n}\nfn b() {\n}\n",
			want: "fn a() {}\n\nfn b() {}\n",
		},
		{
			name: "a blank line is added after the imports",
			in:   "import \"str\"\nimport \"math\"\nfn f() {}\n",
			want: "import \"str\"\nimport \"math\"\n\nfn f() {}\n",
		},
		{
			name: "else and catch stay with the brace they follow",
			in:   "fn f() {\n    if (a) {\n        g()\n    }\n    else {\n        h()\n    }\n}\n",
			want: "fn f() {\n    if (a) {\n        g()\n    } else {\n        h()\n    }\n}\n",
		},
		{
			name: "a trailing comment stays on its line and an own-line comment keeps its indent",
			in:   "fn f() {\n        // why\n    let a: int = 1   // how many\n}\n",
			want: "fn f() {\n    // why\n    let a: int = 1 // how many\n}\n",
		},
		{
			name: "a type argument list is not a comparison",
			in:   "fn f() {\n    let b: Box<int> = Box<int>{value:1}\n}\n",
			want: "fn f() {\n    let b: Box<int> = Box<int> { value: 1 }\n}\n",
		},
		{
			name: "a comparison is not a type argument list",
			in:   "fn f(): bool {\n    return a<b\n}\n",
			want: "fn f(): bool {\n    return a < b\n}\n",
		},
		{
			name: "a lambda body is a block, not a literal",
			in:   "fn f() {\n    let g: fn = fn(x: int): int { return x * 2 }\n}\n",
			want: "fn f() {\n    let g: fn = fn(x: int): int {\n        return x * 2\n    }\n}\n",
		},
		{
			name: "a prefix operator binds to its operand",
			in:   "fn f(): int {\n    return - x + ! flag\n}\n",
			want: "fn f(): int {\n    return -x + !flag\n}\n",
		},
		{
			name: "an interpolated string is rebuilt as written",
			in:   "fn f(): string {\n    return \"a ${ x.name } b ${ n } c\"\n}\n",
			want: "fn f(): string {\n    return \"a ${x.name} b ${n} c\"\n}\n",
		},
		{
			name: "an assignment keeps its right-hand side on the line when a group can open",
			in: "fn f() {\n    let encoded: string = json.encode({alpha: 1, beta: 2, gamma: 3, " +
				"delta: 4, epsilon: 5, zeta: 6, eta: 7, theta: 8, iota: 9})\n}\n",
			want: "fn f() {\n" +
				"    let encoded: string = json.encode({\n" +
				"        alpha: 1,\n" +
				"        beta: 2,\n" +
				"        gamma: 3,\n" +
				"        delta: 4,\n" +
				"        epsilon: 5,\n" +
				"        zeta: 6,\n" +
				"        eta: 7,\n" +
				"        theta: 8,\n" +
				"        iota: 9\n" +
				"    })\n}\n",
		},
		{
			name: "a condition too wide opens its brackets",
			in: "fn f() {\n    if (someLongConditionName == 1 && anotherLongConditionName == 2 && " +
				"aThirdLongConditionNameWithExtraPaddingHere == 3) {\n        g()\n    }\n}\n",
			want: "fn f() {\n" +
				"    if (\n" +
				"        someLongConditionName == 1 && anotherLongConditionName == 2 && aThirdLongConditionNameWithExtraPaddingHere == 3\n" +
				"    ) {\n" +
				"        g()\n" +
				"    }\n}\n",
		},
		{
			name: "operands line up with each other when even that is too wide",
			in: "fn f() {\n    if (someLongConditionName == 1 && anotherLongConditionName == 2 && " +
				"aThirdLongConditionNameWithExtraPaddingHere == 3 && fourthConditionName == 4) {\n        g()\n    }\n}\n",
			want: "fn f() {\n" +
				"    if (\n" +
				"        someLongConditionName == 1 &&\n" +
				"        anotherLongConditionName == 2 &&\n" +
				"        aThirdLongConditionNameWithExtraPaddingHere == 3 &&\n" +
				"        fourthConditionName == 4\n" +
				"    ) {\n" +
				"        g()\n" +
				"    }\n}\n",
		},
		{
			name: "an assignment with no group to open breaks after the operator",
			in: "fn f() {\n    let value: int = someName * otherName + thirdName * fourthName - " +
				"fifthName * sixthName + seventhNameWithEvenMorePaddingX\n}\n",
			want: "fn f() {\n" +
				"    let value: int =\n" +
				"        someName * otherName + thirdName * fourthName - fifthName * sixthName + seventhNameWithEvenMorePaddingX\n" +
				"}\n",
		},
		{
			name: "a field tag survives untouched",
			in:   "struct Row {\n    name: string `db:\"COALESCE(f.name, '')\"`\n}\n",
			want: "struct Row {\n    name: string `db:\"COALESCE(f.name, '')\"`\n}\n",
		},
		{
			name: "an annotation keeps the declaration under it",
			in:   "#[allow(unused)]\nfn f() {}\n",
			want: "#[allow(unused)]\nfn f() {}\n",
		},
		{
			name: "trailing whitespace and a missing final newline are fixed",
			in:   "fn f() {   \n    g()\t\n}",
			want: "fn f() {\n    g()\n}\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := format(t, tc.in)
			if got != tc.want {
				t.Errorf("formatting\n%s\ngave\n%s\nwant\n%s", tc.in, got, tc.want)
			}
			if again := format(t, got); again != got {
				t.Errorf("not idempotent: second pass gave\n%s", again)
			}
		})
	}
}

// TestEmptyAndCommentOnly covers the two files that have no declarations in them
// at all, which is where an off-by-one in the trailing newline would show up.
func TestEmptyAndCommentOnly(t *testing.T) {
	if got := format(t, ""); got != "" {
		t.Errorf("empty file became %q", got)
	}
	if got := format(t, "// just a note\n"); got != "// just a note\n" {
		t.Errorf("comment-only file became %q", got)
	}
	if got := format(t, "\n\n\n"); got != "" {
		t.Errorf("blank file became %q", got)
	}
}

// TestRefusesWhatItCannotLex is the contract the CLI depends on: an error means
// the caller leaves the file alone.
func TestRefusesWhatItCannotLex(t *testing.T) {
	if _, err := Source("fn f() { let s: string = \"unterminated }\n", "bad.dx"); err == nil {
		t.Fatal("expected an error for a file that does not lex")
	}
}

// TestCorpus formats every example in the repository and checks the two
// properties that matter for running this over a codebase unattended: the token
// stream is unchanged — Source itself verifies that — and formatting twice is
// the same as formatting once.
func TestCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "examples", "*.dx"))
	if err != nil || len(files) == 0 {
		t.Skipf("no examples to format (%v)", err)
	}

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		once, err := Source(string(src), file)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
			continue
		}
		twice, err := Source(once, file)
		if err != nil {
			t.Errorf("%s: second pass: %v", filepath.Base(file), err)
			continue
		}
		if once != twice {
			t.Errorf("%s: formatting is not idempotent", filepath.Base(file))
		}
		for i, line := range strings.Split(once, "\n") {
			if strings.TrimRight(line, " \t") != line {
				t.Errorf("%s:%d: trailing whitespace", filepath.Base(file), i+1)
			}
		}
	}
}

// A declaration too wide for one line breaks after the `=` rather than
// spreading a short argument list one-per-line. preferGroup counted any
// multi-argument call as a group worth opening, which turned a two-argument
// call into five lines while the line above it was still the long one.
func TestLongDeclarationBreaksAtAssign(t *testing.T) {
	in := "fn main(): void {\n" +
		"    let certsApi: certificationController.CertificationController = certificationController.newCertificationController(conn, brand)\n" +
		"}\n"
	want := "fn main(): void {\n" +
		"    let certsApi: certificationController.CertificationController =\n" +
		"        certificationController.newCertificationController(conn, brand)\n" +
		"}\n"
	if got := format(t, in); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A sole object literal or lambda still hugs its brackets, which is the case
// preferGroup exists for.
func TestHuggedArgumentStillOpensTheGroup(t *testing.T) {
	in := "fn main(): void {\n" +
		"    let obj: string = json.encode({ message: \"a fairly long message value here to push it over\" })\n" +
		"}\n"
	got := format(t, in)
	if !strings.Contains(got, "json.encode({") {
		t.Errorf("hugged group was not kept on the line:\n%s", got)
	}
}

func TestBlankLineAfterAStatementBlock(t *testing.T) {
	in := `fn getDriver(req: Request): Response {
    let ctx: Ctx = open(req)
    if (ctx.rejected) {
        return denied(ctx)
    }
    if (withheld(ctx)) {
        return unavailable(ctx)
    }
    let home: bool = ctx.home
    let purses: int[] = []
    if (home) {
        purses = owned(ctx)
    } else {
        purses = []
    }
    switch (ctx.kind) {
        case 0: {
            return none()
        }
        default: {
            purses.push(1)
        }
    }
    let doubled: int[] = purses.map(fn(x: int): int {
        return x * 2
    })
    return okFor(ctx, doubled)
}
`
	want := `fn getDriver(req: Request): Response {
    let ctx: Ctx = open(req)
    if (ctx.rejected) {
        return denied(ctx)
    }

    if (withheld(ctx)) {
        return unavailable(ctx)
    }

    let home: bool = ctx.home
    let purses: int[] = []
    if (home) {
        purses = owned(ctx)
    } else {
        purses = []
    }

    switch (ctx.kind) {
        case 0: {
            return none()
        }
        default: {
            purses.push(1)
        }
    }

    let doubled: int[] = purses.map(fn(x: int): int {
        return x * 2
    })
    return okFor(ctx, doubled)
}
`
	if got := format(t, in); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := format(t, want); got != want {
		t.Errorf("not idempotent:\n%s", got)
	}
}

// assertFormats checks in formats to exactly want, and that want is a fixed
// point: formatting it again changes nothing.
func assertFormats(t *testing.T, in, want string) {
	t.Helper()
	if got := format(t, in); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if again := format(t, want); again != want {
		t.Errorf("not idempotent; formatting the result again gave:\n%s", again)
	}
}

// A method chain that does not fit breaks at its dots, one link per line,
// rather than inside the first call's parentheses. Written one link per line or
// all on one, it comes out the same: the layout is a function of the code.
func TestLongChainBreaksAtItsDots(t *testing.T) {
	want := `fn main(): void {
    let byAuthor: q.Query = q.from("books")
        .columns(["title"])
        .where(q.col("author_name").eq(q.param("who")))
        .build(LITE)
    let reprint: q.Insert = q.insertInto("books")
        .columns(["title", "author_name", "pages"])
        .values([q.text("Dune"), q.text("Herbert"), q.integer(896)])
        .onConflict(["title"])
        .doUpdate(["pages"])
    let short: q.Select = q.from("t").limit(1)
}
`
	asWritten := `fn main(): void {
    let byAuthor: q.Query = q.from("books")
        .columns(["title"])
        .where(q.col("author_name").eq(q.param("who")))
        .build(LITE)
    let reprint: q.Insert = q.insertInto("books")
        .columns(["title", "author_name", "pages"])
        .values([q.text("Dune"), q.text("Herbert"), q.integer(896)])
        .onConflict(["title"])
        .doUpdate(["pages"])
    let short: q.Select = q.from("t")
        .limit(1)
}
`
	oneLine := `fn main(): void {
    let byAuthor: q.Query = q.from("books").columns(["title"]).where(q.col("author_name").eq(q.param("who"))).build(LITE)
    let reprint: q.Insert = q.insertInto("books").columns(["title", "author_name", "pages"]).values([q.text("Dune"), q.text("Herbert"), q.integer(896)]).onConflict(["title"]).doUpdate(["pages"])
    let short: q.Select = q.from("t").limit(1)
}
`
	assertFormats(t, asWritten, want)
	assertFormats(t, oneLine, want)
}

// Two links are enough: a two-link builder chain reads better one per line too.
func TestTwoLinkChainBreaksAtItsDots(t *testing.T) {
	in := `fn main(): void {
    let req: Request = client.request("GET", "https://api.example.com/v1/books").header("Authorization", bearer).timeout(30000)
}
`
	want := `fn main(): void {
    let req: Request = client.request("GET", "https://api.example.com/v1/books")
        .header("Authorization", bearer)
        .timeout(30000)
}
`
	assertFormats(t, in, want)
}

// A chain starting from a bare call keeps that call as its head, and so does a
// statement that is not an assignment.
func TestBareCallHeadStaysOnTheLine(t *testing.T) {
	in := `fn main(): void {
    return build().where(q.col("author_name").eq(q.param("who"))).orderBy("title", "published_on", "pages").limit(pageSize)
}
`
	want := `fn main(): void {
    return build()
        .where(q.col("author_name").eq(q.param("who")))
        .orderBy("title", "published_on", "pages")
        .limit(pageSize)
}
`
	assertFormats(t, in, want)
}

// A link too wide for its own line opens its argument under the link, by the
// same rules as any other segment.
func TestLongLinkOpensItsOwnArgument(t *testing.T) {
	in := `fn main(): void {
    let reprint: q.Insert = q.insertInto("books").values([q.text("Dune: the original and still the best of them"), q.text("Frank Patrick Herbert"), q.integer(896)]).onConflict(["title"])
}
`
	want := `fn main(): void {
    let reprint: q.Insert = q.insertInto("books")
        .values([
            q.text("Dune: the original and still the best of them"),
            q.text("Frank Patrick Herbert"),
            q.integer(896)
        ])
        .onConflict(["title"])
}
`
	assertFormats(t, in, want)
}

// Inside an expanded argument list the links step in from their head. Indented
// at the argument's own column they would read as further arguments.
func TestChainInsideAnArgumentIndentsFromItsHead(t *testing.T) {
	in := `fn main(): void {
    run(conn, q.from("books").columns(["title", "author_name", "pages"]).where(q.col("author_name").eq(q.param("who"))).build(LITE), args)
}
`
	want := `fn main(): void {
    run(
        conn,
        q.from("books")
            .columns(["title", "author_name", "pages"])
            .where(q.col("author_name").eq(q.param("who")))
            .build(LITE),
        args
    )
}
`
	assertFormats(t, in, want)
}

// One link is not a chain: the line opens a group as before. A module
// qualifier and a field access are not links either.
func TestOneLinkIsNotAChain(t *testing.T) {
	in := `fn main(): void {
    let rows: Row[] = self.store.queryAll("SELECT title, author_name, pages FROM books WHERE pages > 300").filter(longEnough, alsoPinned)
}
`
	got := format(t, in)
	if strings.Contains(got, "\n        .filter(") {
		t.Errorf("a single link was broken at its dot:\n%s", got)
	}
	if again := format(t, got); again != got {
		t.Errorf("not idempotent:\n%s", again)
	}
}
