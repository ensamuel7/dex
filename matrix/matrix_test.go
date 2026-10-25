package matrix

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type valueType struct {
	name    string
	typ     string
	lit     string
	decls   string
	ok      string
	quick   string
	isArray bool
}

type producer struct {
	name  string
	decls string
	setup string
	expr  string
	src   string
	param bool
	only  string
}

type consumer struct {
	name  string
	decls string
	body  string
	ret   string
}

const recDecl = `struct Rec {
    name: string
    tags: string[]
    n: int
}
`

const recLit = `Rec { name: "na" + "me", tags: ["t" + "1"], n: 3 }`

var valueTypes = []valueType{
	{name: "string", typ: "string", lit: `"val" + "ue"`, ok: `return v == "value"`, quick: `{v} == "value"`},
	{name: "intArray", typ: "int[]", lit: `[7, 8]`, ok: `return v.len() == 2 && v[0] == 7 && v[1] == 8`, quick: `{v}.len() == 2`, isArray: true},
	{name: "stringArray", typ: "string[]", lit: `["a" + "b", "cd"]`, ok: `return v.len() == 2 && v[0] == "ab" && v[1] == "cd"`, quick: `{v}[0] == "ab"`, isArray: true},
	{name: "struct", typ: "Rec", lit: recLit, decls: recDecl, ok: `return v.name == "name" && v.tags[0] == "t1" && v.n == 3`, quick: `{v}.name == "name"`},
	{name: "nestedStruct", typ: "Outer", lit: `Outer { inner: Inner { label: "lb" + "l" }, id: 4 }`,
		decls: "struct Inner {\n    label: string\n}\n\nstruct Outer {\n    inner: Inner\n    id: int\n}\n",
		ok:    `return v.inner.label == "lbl" && v.id == 4`, quick: `{v}.inner.label == "lbl"`},
	{name: "structArray", typ: "Rec[]", lit: "[" + recLit + ", " + recLit + "]", decls: recDecl,
		ok: `return v.len() == 2 && v[0].name == "name" && v[1].tags[0] == "t1"`, quick: `{v}[1].name == "name"`, isArray: true},
	{name: "map", typ: "map[string, string]", lit: `mkMap()`,
		decls: "fn mkMap(): map[string, string] {\n    let m: map[string, string] = {}\n    m[\"k\"] = \"v\" + \"al\"\n    return m\n}\n",
		ok:    `return v.len() == 1 && v["k"] == "val"`, quick: `{v}["k"] == "val"`},
	{name: "optStruct", typ: "Rec?", lit: recLit, decls: recDecl,
		ok: "if (v != null) {\n        return v.name == \"name\" && v.tags[0] == \"t1\"\n    }\n    return false", quick: `okv({v})`},
	{name: "optString", typ: "string?", lit: `"val" + "ue"`,
		ok: "if (v != null) {\n        return v == \"value\"\n    }\n    return false", quick: `{v} != null`},
	{name: "interface", typ: "Greeter", lit: `Person { name: "na" + "me" }`,
		decls: "interface Greeter {\n    fn greet(): string\n}\n\nstruct Person {\n    name: string\n\n    fn greet(): string {\n        return \"hi \" + name\n    }\n}\n",
		ok:    `return v.greet() == "hi name"`, quick: `{v}.greet() == "hi name"`},
	{name: "closure", typ: "fn(int): int", lit: `fn(x: int): int { return x + 5 }`, ok: `return v(2) == 7`, quick: `okv({v})`},
	{name: "json", typ: "json.Value", lit: `{ name: "d" + "ex", n: 3 }`,
		ok: `return v["name"].asString() == "dex" && v["n"].asInt() == 3`, quick: `{v}["name"].asString() == "dex"`},
	{name: "payloadEnum", typ: "Shape", lit: `Shape.Label("lab" + "el")`,
		decls: "enum Shape {\n    Empty\n    Label(string)\n    Tags(string[])\n}\n",
		ok:    "switch (v) {\n        case Shape.Label(t): {\n            return t == \"label\"\n        }\n        default: {\n            return false\n        }\n    }\n    return false", quick: `okv({v})`},
}

var producers = []producer{
	{name: "lit", expr: "{LIT}", src: "true"},
	{name: "call", decls: "fn make(): {T} {\n    return {LIT}\n}\n", expr: "make()", src: "true"},
	{name: "var", setup: "let y: {T} = {LIT}", expr: "y", src: "okv(y)"},
	{name: "field", decls: "struct Hold {\n    v: {T}\n}\n", setup: "let h: Hold = Hold { v: {LIT} }", expr: "h.v", src: "okv(h.v)"},
	{name: "nestedField", decls: "struct Hold {\n    v: {T}\n}\n\nstruct Hold2 {\n    h: Hold\n}\n",
		setup: "let hh: Hold2 = Hold2 { h: Hold { v: {LIT} } }", expr: "hh.h.v", src: "okv(hh.h.v)"},
	{name: "fieldOfCall", decls: "struct Hold {\n    v: {T}\n}\n\nfn mkHold(): Hold {\n    return Hold { v: {LIT} }\n}\n", expr: "mkHold().v", src: "true"},
	{name: "getter", decls: "struct Getter {\n    v: {T}\n\n    fn get(): {T} {\n        return self.v\n    }\n}\n",
		setup: "let g: Getter = Getter { v: {LIT} }", expr: "g.get()", src: "okv(g.v)"},
	{name: "index", setup: "let arr: {T}[] = [{LIT}]", expr: "arr[0]", src: "okv(arr[0])"},
	{name: "param", expr: "p", src: "okv(p)", param: true},
	{name: "match", expr: "match (i % 2) {\n        0 => {LIT}\n        _ => {LIT}\n    }", src: "true"},
	{name: "matchVar", setup: "let y: {T} = {LIT}", expr: "match (i % 2) {\n        0 => y\n        _ => y\n    }", src: "okv(y)"},
	{name: "methodValue", only: "closure",
		decls: "struct Counter {\n    step: int\n    label: string\n\n    fn bump(x: int): int {\n        return x + step\n    }\n}\n",
		setup: "let c: Counter = Counter { step: 5, label: \"l\" + \"b\" }", expr: "c.bump", src: "c.label == \"lb\""},
}

const midDecl = "fn boom(i: int): void {\n    if (i >= 0) {\n        throw Exception(\"boom\")\n    }\n}\n"

var consumers = []consumer{
	{name: "let", body: "let x: {T} = {X}\n    return okv(x) && {S}"},
	{name: "letInfer", body: "let x = {X}\n    return okv(x) && {S}"},
	{name: "reassign", body: "let x: {T} = {LIT}\n    x = {X}\n    return okv(x) && {S}"},
	{name: "reassignLoop", body: "let x: {T} = {LIT}\n    for (let k: int = 0; k < 3; k++) {\n        x = {X}\n    }\n    return okv(x) && {S}"},
	{name: "arg", decls: "fn take(v: {T}): bool {\n    return okv(v)\n}\n", body: "let ok: bool = take({X})\n    return ok && {S}"},
	{name: "discard", decls: "fn keep(v: {T}): {T} {\n    return v\n}\n", body: "keep({X})\n    return {S}"},
	{name: "return", ret: "T", body: "return {X}"},
	{name: "returnFieldLit", ret: "Wrap", decls: "struct Wrap {\n    v: {T}\n}\n", body: "return Wrap { v: {X} }"},
	{name: "returnFieldCtor", ret: "Wrap", decls: "struct Wrap(v: {T}) {}\n", body: "return Wrap({X})"},
	{name: "returnArrayLit", ret: "Arr", body: "return [{X}]"},
	{name: "returnOptional", ret: "Opt", body: "return {X}"},
	{name: "fieldLit", decls: "struct Wrap {\n    v: {T}\n}\n", body: "let w: Wrap = Wrap { v: {X} }\n    return okv(w.v) && {S}"},
	{name: "fieldCtor", decls: "struct Wrap(v: {T}) {}\n", body: "let w: Wrap = Wrap({X})\n    return okv(w.v) && {S}"},
	{name: "fieldAssign", decls: "struct Wrap {\n    v: {T}\n}\n", body: "let w: Wrap = Wrap { v: {LIT} }\n    w.v = {X}\n    return okv(w.v) && {S}"},
	{name: "arrayLit", body: "let a: {T}[] = [{X}]\n    return okv(a[0]) && {S}"},
	{name: "arrayLitSecond", body: "let a: {T}[] = [{LIT}, {X}]\n    return okv(a[0]) && okv(a[1]) && {S}"},
	{name: "push", body: "let a: {T}[] = []\n    a.push({X})\n    return okv(a[0]) && {S}"},
	{name: "indexAssign", body: "let a: {T}[] = [{LIT}]\n    a[0] = {X}\n    return okv(a[0]) && {S}"},
	{name: "mapValue", body: "let m: map[string, {T}] = {}\n    m[\"k\"] = {X}\n    return okv(m[\"k\"]) && {S}"},
	{name: "mapSet", body: "let m: map[string, {T}] = {}\n    m.set(\"k\", {X})\n    return okv(m.get(\"k\")) && {S}"},
	{name: "optWrap", body: "let o: {T}? = {X}\n    if (o != null) {\n        return okv(o) && {S}\n    }\n    return false"},
	{name: "ifQuick", body: "if ({Q}) {\n        return {S}\n    }\n    return false"},
	{name: "ifCall", body: "if (okv({X})) {\n        return {S}\n    }\n    return false"},
	{name: "whileCond", body: "let n: int = 0\n    while (n < 3 && okv({X})) {\n        n++\n    }\n    return n == 3 && {S}"},
	{name: "andRight", body: "let c: bool = i >= 0 && {Q}\n    return c && {S}"},
	{name: "andSkipped", body: "let c: bool = i < 0 && {Q}\n    return !c && {S}"},
	{name: "orRight", body: "let c: bool = i < 0 || okv({X})\n    return c && {S}"},
	{name: "interp", body: "let s: string = \"<${{X}}>\"\n    let lv: {T} = {LIT}\n    let t: string = \"<${lv}>\"\n    return s == t && {S}"},
	{name: "concat", body: "let s: string = \"<\" + {X}\n    let lv: {T} = {LIT}\n    return s == \"<\" + lv && {S}"},
	{name: "matchArm", body: "let z: {T} = match (i % 2) {\n        0 => {X}\n        _ => {X}\n    }\n    return okv(z) && {S}"},
	{name: "defer", decls: "fn sink(v: {T}): void {\n    assert(okv(v))\n}\n", body: "defer sink({X})\n    return {S}"},
	{name: "deferLoop", decls: "fn sink(v: {T}): void {\n    assert(okv(v))\n}\n", body: "for (let k: int = 0; k < 3; k++) {\n        defer sink({X})\n    }\n    return {S}"},
	{name: "spawnLet", body: "let x: {T} = {X}\n    let t = spawn {\n        send(okv(x))\n    }\n    let r: bool = receive(t)\n    return r && okv(x) && {S}"},
	{name: "spawnDirect", body: "let t = spawn {\n        send(okv({X}))\n    }\n    let r: bool = receive(t)\n    return r && {S}"},
	{name: "spawnCall", decls: "fn take(v: {T}): bool {\n    return okv(v)\n}\n", body: "let t = spawn take({X})\n    let r: bool = receive(t)\n    return r && {S}"},
	{name: "lambdaLet", body: "let x: {T} = {X}\n    let f: fn(): bool = fn(): bool { return okv(x) }\n    return f() && okv(x) && {S}"},
	{name: "lambdaDirect", body: "let f: fn(): bool = fn(): bool { return okv({X}) }\n    return f() && f() && {S}"},
	{name: "lambdaEscape", ret: "Fn", body: "let x: {T} = {X}\n    return fn(): bool { return okv(x) }"},
	{name: "lambdaEscapeDirect", ret: "Fn", body: "return fn(): bool { return okv({X}) }"},
	{name: "tryLocal", decls: midDecl, body: "try {\n        let x: {T} = {X}\n        boom(i)\n        return okv(x)\n    } catch (e: Exception) {\n    }\n    return {S}"},
	{name: "tryReassign", decls: midDecl, body: "let x: {T} = {LIT}\n    try {\n        x = {X}\n        boom(i)\n    } catch (e: Exception) {\n    }\n    return okv(x) && {S}"},
	{name: "tryFrame", decls: midDecl + "\nfn mid(v: {T}, i: int): bool {\n    let y: {T} = v\n    boom(i)\n    return okv(y)\n}\n",
		body: "try {\n        mid({X}, i)\n    } catch (e: Exception) {\n    }\n    return {S}"},
	{name: "tryFrameStruct", decls: midDecl + "\nstruct Wrap {\n    v: {T}\n}\n\nfn mid(v: {T}, i: int): bool {\n    let w: Wrap = Wrap { v: v }\n    boom(i)\n    return okv(w.v)\n}\n",
		body: "try {\n        mid({X}, i)\n    } catch (e: Exception) {\n    }\n    return {S}"},
	{name: "tryDefer", decls: midDecl + "\nfn sink(v: {T}): void {\n    assert(okv(v))\n}\n\nfn mid(v: {T}, i: int): bool {\n    defer sink(v)\n    boom(i)\n    return true\n}\n",
		body: "try {\n        mid({X}, i)\n    } catch (e: Exception) {\n    }\n    return {S}"},
	{name: "foreach", body: "let n: int = 0\n    foreach ({F} as e) {\n        n++\n    }\n    return n == {FN} && {S}"},
	{name: "foreachCheck", body: "let n: int = 0\n    foreach ([{X}] as e) {\n        if (okv(e)) {\n            n++\n        }\n    }\n    return n == 1 && {S}"},
}

type combo struct {
	vt   valueType
	pr   producer
	co   consumer
	name string
}

func (c combo) source(iters int) string {
	vt, pr, co := c.vt, c.pr, c.co
	typeRepl := strings.NewReplacer("{T}", vt.typ, "{LIT}", vt.lit)
	expr := typeRepl.Replace(pr.expr)
	src := typeRepl.Replace(pr.src)
	quick := strings.ReplaceAll(vt.quick, "{v}", expr)
	foreachSrc, foreachN := "["+expr+"]", "1"
	if vt.isArray {
		foreachSrc, foreachN = expr, "2"
	}
	bodyRepl := strings.NewReplacer("{T}", vt.typ, "{LIT}", vt.lit, "{X}", expr, "{S}", src, "{Q}", quick, "{F}", foreachSrc, "{FN}", foreachN)

	var b strings.Builder
	if strings.Contains(vt.typ, "json.") {
		b.WriteString("import \"json\"\n\n")
	}
	seen := map[string]bool{}
	addDecl := func(d string) {
		d = typeRepl.Replace(d)
		for _, part := range strings.Split(d, "\n\n") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			b.WriteString(part)
			b.WriteString("\n\n")
		}
	}
	addDecl(vt.decls)
	addDecl(pr.decls)
	addDecl(co.decls)
	addDecl("fn okv(v: {T}): bool {\n    " + vt.ok + "\n}\n")

	ret := "bool"
	switch co.ret {
	case "T":
		ret = vt.typ
	case "Wrap":
		ret = "Wrap"
	case "Arr":
		ret = vt.typ + "[]"
	case "Opt":
		ret = vt.typ + "?"
	case "Fn":
		ret = "fn(): bool"
	}
	params := "i: int"
	if pr.param {
		params += ", p: " + vt.typ
	}
	fmt.Fprintf(&b, "fn site(%s): %s {\n", params, ret)
	if pr.setup != "" {
		b.WriteString("    " + typeRepl.Replace(pr.setup) + "\n")
	}
	b.WriteString("    " + bodyRepl.Replace(co.body) + "\n}\n\n")

	call := "site(i)"
	if pr.param {
		call = "site(i, p)"
	}
	fmt.Fprintf(&b, "fn main(): void {\n    for (let i: int = 0; i < %d; i++) {\n", iters)
	if pr.param {
		b.WriteString("        let p: " + vt.typ + " = " + vt.lit + "\n")
	}
	switch co.ret {
	case "T":
		fmt.Fprintf(&b, "        let r: %s = %s\n        assert(okv(r))\n", vt.typ, call)
	case "Wrap":
		fmt.Fprintf(&b, "        let r: Wrap = %s\n        assert(okv(r.v))\n", call)
	case "Arr":
		fmt.Fprintf(&b, "        let r: %s[] = %s\n        assert(okv(r[0]))\n", vt.typ, call)
	case "Opt":
		fmt.Fprintf(&b, "        let r: %s? = %s\n        if (r != null) {\n            assert(okv(r))\n        } else {\n            assert(false)\n        }\n", vt.typ, call)
	case "Fn":
		fmt.Fprintf(&b, "        let f: fn(): bool = %s\n        assert(f())\n        assert(f())\n", call)
	default:
		fmt.Fprintf(&b, "        assert(%s)\n", call)
	}
	if pr.param {
		b.WriteString("        assert(okv(p))\n")
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

func allCombos() []combo {
	var out []combo
	for _, vt := range valueTypes {
		for _, pr := range producers {
			if pr.only != "" && pr.only != vt.name {
				continue
			}
			for _, co := range consumers {
				out = append(out, combo{vt: vt, pr: pr, co: co, name: vt.name + "_" + pr.name + "_" + co.name})
			}
		}
	}
	return out
}

var sample = map[string]bool{
	"string_call_arg":                 true,
	"string_lit_tryReassign":          true,
	"string_lit_spawnCall":            true,
	"string_fieldOfCall_fieldAssign":  true,
	"struct_field_returnFieldCtor":    true,
	"struct_lit_indexAssign":          true,
	"struct_call_interp":              true,
	"nestedStruct_param_lambdaEscape": true,
	"structArray_index_push":          true,
	"optStruct_getter_let":            true,
	"optString_var_fieldAssign":       true,
	"interface_call_arrayLit":         true,
	"interface_var_returnArrayLit":    true,
	"interface_param_returnFieldLit":  true,
	"closure_lit_tryReassign":         true,
	"closure_methodValue_spawnLet":    true,
	"json_lit_tryLocal":               true,
	"payloadEnum_matchVar_defer":      true,
	"payloadEnum_lit_optWrap":         true,
	"map_fieldOfCall_ifCall":          true,
	"stringArray_nestedField_foreach": true,
	"intArray_fieldOfCall_foreach":    true,
}

type outcome int

const (
	outPass outcome = iota
	outInvalid
	outCompileFail
	outRunFail
)

type result struct {
	c      combo
	out    outcome
	detail string
	src    string
}

var knownGaps = map[string]string{}

func repoRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd)
}

func buildDex(t *testing.T, dir string) string {
	bin := filepath.Join(dir, "dex")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building dex: %v\n%s", err, out)
	}
	return bin
}

func runCombo(dex, root string, c combo, iters int, asan bool) result {
	src := c.source(iters)
	r := result{c: c, src: src}
	dir := filepath.Join(root, c.name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.out, r.detail = outCompileFail, err.Error()
		return r
	}
	file := filepath.Join(dir, "m.dx")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		r.out, r.detail = outCompileFail, err.Error()
		return r
	}
	env := os.Environ()
	if asan {
		env = append(env, "DEX_SANITIZE=1")
	} else {
		env = append(env, "DEX_MEM_CHECK=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dex, "build", "--no-lint", "m.dx")
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := string(out)
		switch {
		case strings.Contains(text, "C compilation failed"), strings.Contains(text, "panic:"), ctx.Err() != nil:
			r.out, r.detail = outCompileFail, text
		default:
			r.out, r.detail = outInvalid, firstLine(text)
		}
		return r
	}
	rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer rcancel()
	run := exec.CommandContext(rctx, filepath.Join(dir, "build", "m"))
	run.Dir = dir
	run.Env = append(os.Environ(), "DEX_MEM_LEAKS=error")
	if asan && runtime.GOOS == "linux" {
		run.Env = append(run.Env, "ASAN_OPTIONS=detect_leaks=1")
	}
	var buf bytes.Buffer
	run.Stdout = &buf
	run.Stderr = &buf
	if err := run.Run(); err != nil {
		r.out, r.detail = outRunFail, fmt.Sprintf("%v\n%s", err, buf.String())
		return r
	}
	r.out = outPass
	os.RemoveAll(dir)
	return r
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestOwnershipMatrix(t *testing.T) {
	full := os.Getenv("DEX_MATRIX") == "1"
	asan := os.Getenv("DEX_MATRIX_ASAN") == "1"
	filter := os.Getenv("DEX_MATRIX_FILTER")
	iters := 50

	var combos []combo
	for _, c := range allCombos() {
		if filter != "" {
			if strings.Contains(c.name, filter) {
				combos = append(combos, c)
			}
			continue
		}
		if full || sample[c.name] {
			combos = append(combos, c)
		}
	}
	if len(combos) == 0 {
		t.Skip("no combinations selected")
	}

	tmp := t.TempDir()
	dex := buildDex(t, tmp)
	work := filepath.Join(tmp, "work")

	jobs := make(chan combo)
	results := make(chan result)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				results <- runCombo(dex, work, c, iters, asan)
			}
		}()
	}
	go func() {
		for _, c := range combos {
			jobs <- c
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var all []result
	for r := range results {
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].c.name < all[j].c.name })

	outDir := os.Getenv("DEX_MATRIX_OUT")
	if outDir != "" {
		os.MkdirAll(outDir, 0o755)
	}
	counts := map[outcome]int{}
	known := 0
	var invalidLog strings.Builder
	for _, r := range all {
		if r.out == outCompileFail || r.out == outRunFail {
			if reason, ok := knownGaps[r.c.name]; ok {
				known++
				t.Logf("known gap %s: %s", r.c.name, reason)
				continue
			}
		}
		counts[r.out]++
		switch r.out {
		case outInvalid:
			fmt.Fprintf(&invalidLog, "%s\t%s\n", r.c.name, r.detail)
		case outCompileFail, outRunFail:
			kind := "compile"
			if r.out == outRunFail {
				kind = "run"
			}
			t.Errorf("%s failure: %s\n%s", kind, r.c.name, tail(r.detail, 25))
			if outDir != "" {
				os.WriteFile(filepath.Join(outDir, r.c.name+".dx"), []byte(r.src), 0o644)
				os.WriteFile(filepath.Join(outDir, r.c.name+".log"), []byte(r.detail), 0o644)
			}
		}
	}
	if outDir != "" {
		os.WriteFile(filepath.Join(outDir, "invalid.tsv"), []byte(invalidLog.String()), 0o644)
	}
	t.Logf("generated %d, invalid Dex skipped %d, passed %d, failed %d (compile %d, run %d), known gaps %d",
		len(all), counts[outInvalid], counts[outPass], counts[outCompileFail]+counts[outRunFail],
		counts[outCompileFail], counts[outRunFail], known)
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestMatrixSourceIsDeterministic(t *testing.T) {
	cs := allCombos()
	if len(cs) == 0 {
		t.Fatal("empty matrix")
	}
	for name := range sample {
		found := false
		for _, c := range cs {
			if c.name == name {
				found = true
				if c.source(1) != c.source(1) {
					t.Fatalf("%s: source differs between calls", name)
				}
			}
		}
		if !found {
			t.Errorf("sample names a missing combination %s", name)
		}
	}
}
