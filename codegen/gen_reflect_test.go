package codegen

import (
	"fmt"
	"strings"
	"testing"
)

// Reflection is opt-in: a program that does not use it must be exactly what it
// was before the module existed.
func TestReflectEmitsNothingWhenUnused(t *testing.T) {
	c := generate(t, `import "fmt"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		fmt.println(unit.tag)
	}`)
	if strings.Contains(c, "_dex_refl") {
		t.Error("emitted reflection metadata for a program that never reflects")
	}
}

// Metadata is per struct, not per program: reflecting one struct must not put
// tables on the others.
func TestReflectEmitsMetadataOnlyForReflectedStructs(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct Reflected { a: int  b: string }
	struct Untouched { c: int  d: string }
	fn main(): void {
		let r: Reflected = Reflected { a: 1, b: "x" }
		let u: Untouched = Untouched { c: 2, d: "y" }
		fmt.println(reflect.fieldName(r, 0))
		fmt.println(u.d)
	}`)
	if !strings.Contains(c, "_dex_refl_names_Reflected") {
		t.Error("missing metadata for the reflected struct")
	}
	if strings.Contains(c, "_dex_refl_names_Untouched") {
		t.Error("emitted metadata for a struct no reflect call named")
	}
}

// The field count is known while compiling, so it must appear as a literal
// rather than a call — that is what lets a loop over it be unrolled.
func TestReflectFieldCountIsAConstant(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string  kw: double }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t", kw: 2.0 }
		fmt.println(reflect.fieldCount(unit))
	}`)
	if !strings.Contains(c, `printf("%d\n", 3)`) {
		t.Errorf("fieldCount did not fold to a literal 3:\n%s", c)
	}
}

// A literal field name folds to its index, so a reflective read of a known
// field costs what a field access costs.
func TestReflectIndexOfFoldsLiteralName(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		fmt.println(reflect.asString(unit, reflect.indexOf(unit, "tag")))
	}`)
	if !strings.Contains(c, "_dex_refl_str_User(&(unit), 1)") {
		t.Errorf("indexOf of a literal name did not fold to the constant 1:\n%s", c)
	}
	// The runtime's definition is always present once reflect is imported; what
	// must be absent is a call to it.
	if strings.Contains(c, "dex_refl_index_of((") {
		t.Error("emitted a runtime name search for a literal name")
	}
}

// A name only known at runtime is the one case that has to search the table.
func TestReflectIndexOfSearchesComputedName(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		let picked: string = "ta" + "g"
		fmt.println(reflect.indexOf(unit, picked))
	}`)
	if !strings.Contains(c, "dex_refl_index_of((") {
		t.Errorf("a computed name should search the name table:\n%s", c)
	}
}

// Only fields of the accessor's own type become cases, so a mismatched read
// falls to the default and returns a zero value instead of reinterpreting the
// bytes of a pointer as an integer.
func TestReflectAccessorsAreTypeSeparated(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		fmt.println(reflect.asLong(unit, 0))
		fmt.println(reflect.asString(unit, 1))
	}`)
	longFn := sliceFunc(c, "_dex_refl_long_User")
	if strings.Contains(longFn, "s->tag") {
		t.Errorf("the long accessor reads a string field:\n%s", longFn)
	}
	strFn := sliceFunc(c, "_dex_refl_str_User")
	if strings.Contains(strFn, "s->id") {
		t.Errorf("the string accessor reads a long field:\n%s", strFn)
	}
	// Reading a string field through reflection mints a reference, because
	// isNewAlloc treats every call as minting and the consumer will release it.
	if !strings.Contains(strFn, "dex_retained(s->tag)") {
		t.Errorf("the string accessor must retain what it hands back:\n%s", strFn)
	}
}

// sliceFunc returns the text of the named generated function.
func sliceFunc(c, name string) string {
	start := strings.Index(c, name+"(const")
	if start < 0 {
		return ""
	}
	end := strings.Index(c[start:], "\n}")
	if end < 0 {
		return c[start:]
	}
	return c[start : start+end]
}

// A metadata-only call answers from the subject's type and never reads its
// value, so without care the subject expression would not be emitted at all and
// a subject that does work would never run.
func TestReflectEvaluatesSubjectOfMetadataOnlyCall(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn pick(): int { return 0 }
	fn main(): void {
		let people: User[] = [User { id: 1, tag: "a" }]
		fmt.println(reflect.fieldName(people[pick()], 1))
	}`)
	if !strings.Contains(c, "(((void)(") || !strings.Contains(c, "pick()") {
		t.Errorf("the subject expression was dropped instead of evaluated:\n%s", c)
	}
}

// A plain variable cannot do work, so it is not evaluated-and-discarded first.
func TestReflectDoesNotWrapPlainVariableSubject(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "a" }
		fmt.println(reflect.fieldName(unit, 1))
	}`)
	if strings.Contains(c, "(((void)(unit))") {
		t.Errorf("needlessly evaluated a plain variable subject:\n%s", c)
	}
}

// Tags are stored verbatim in a table parallel to the names, so an untagged
// field still occupies its slot and the indices stay aligned.
func TestReflectEmitsTagTable(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct S {
		id: long
		name: string `+"`"+`db:"COALESCE(f.name, '')"`+"`"+`
	}
	fn main(): void {
		let s: S = S { id: 1, name: "x" }
		fmt.println(reflect.fieldTag(s, 1, "db"))
	}`)
	if !strings.Contains(c, "_dex_refl_tags_S") {
		t.Errorf("no tag table emitted:\n%s", c)
	}
	// The untagged field holds "", keeping the table aligned with the names.
	if !strings.Contains(c, `_dex_refl_tags_S[2] = {"", "db:\"COALESCE(f.name, '')\""}`) {
		t.Errorf("tag table is not verbatim and index-aligned:\n%s", c)
	}
	if !strings.Contains(c, `_dex_refl_tag_S(1, "db")`) {
		t.Errorf("fieldTag did not fold its literal key:\n%s", c)
	}
}

// A struct with no tags at all still gets the table, so fieldTag answers ""
// rather than needing a special case at every call.
func TestReflectTagTableForUntaggedStruct(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct P { x: int  y: int }
	fn main(): void {
		let p: P = P { x: 1, y: 2 }
		fmt.println(reflect.fieldTag(p, 0, "db"))
	}`)
	if !strings.Contains(c, `_dex_refl_tags_P[2] = {"", ""}`) {
		t.Errorf("untagged struct should still have an aligned empty table:\n%s", c)
	}
}

// --- Writers ---------------------------------------------------------------

const reflectSetProgram = `import "fmt"
	import "reflect"
	struct User { id: long  tag: string  n: int }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t", n: 2 }
		fmt.println(reflect.setLong(unit, 0, 5))
		fmt.println(reflect.setString(unit, 1, "x"))
	}`

// A setter lists only the fields of its own kind, the same thing that makes a
// mismatched read return a zero value instead of garbage.
func TestReflectSettersAreTypeSeparated(t *testing.T) {
	c := generate(t, reflectSetProgram)
	longFn := sliceMutFunc(c, "_dex_refl_set_long_User")
	if longFn == "" {
		t.Fatalf("no long setter emitted:\n%s", c)
	}
	if !strings.Contains(longFn, "s->id = v") {
		t.Errorf("the long setter does not write the long field:\n%s", longFn)
	}
	for _, other := range []string{"s->tag", "s->n"} {
		if strings.Contains(longFn, other) {
			t.Errorf("the long setter writes %s, a field of another kind:\n%s", other, longFn)
		}
	}
	intFn := sliceMutFunc(c, "_dex_refl_set_int_User")
	if !strings.Contains(intFn, "s->n = v") {
		t.Errorf("the int setter does not write the int field:\n%s", intFn)
	}
}

// A struct owns its string fields, so a write releases what the field held and
// takes a reference to what replaced it — retain first, because the two can be
// the same object.
func TestReflectStringSetterBalancesOwnership(t *testing.T) {
	c := generate(t, reflectSetProgram)
	strFn := sliceMutFunc(c, "_dex_refl_set_str_User")
	want := "dex_retain(v); dex_release(s->tag); s->tag = v;"
	if !strings.Contains(strFn, want) {
		t.Errorf("string setter must retain the new value before releasing the old.\nwant: %s\ngot:\n%s", want, strFn)
	}
	if strings.Index(strFn, "dex_release(s->tag)") < strings.Index(strFn, "dex_retain(v)") {
		t.Errorf("release must come after retain, or writing a field from itself drops its last reference:\n%s", strFn)
	}
}

// The setter takes its own reference, so a value built for the call is hoisted
// into a statement-scoped temporary and released — not handed over.
func TestReflectStringSetterReleasesMintedValue(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	import "str"
	struct User { tag: string }
	fn main(): void {
		let unit: User = User { tag: "t" }
		fmt.println(reflect.setString(unit, 0, "a" + str.fromInt(1)))
	}`)
	line := ""
	for _, l := range strings.Split(c, "\n") {
		if strings.Contains(l, "_dex_refl_set_str_User(") && !strings.Contains(l, "static") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no call to the string setter:\n%s", c)
	}
	if !strings.Contains(line, "dex_release(_dex_tmp_") {
		t.Errorf("a minted value passed to setString is never released:\n%s", line)
	}
}

// A &Struct subject is already a pointer; a local has its address taken. Either
// way the write must land on the caller's struct, never on a copy.
func TestReflectSetterSubjectIsNotCopied(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { tag: string }
	fn fill(out: &User): void { fmt.println(reflect.setString(out, 0, "x")) }
	fn main(): void {
		let unit: User = User { tag: "t" }
		fill(unit)
		fmt.println(reflect.setString(unit, 0, "y"))
	}`)
	assertContains(t, c, "_dex_refl_set_str_User(out, 0,")
	assertContains(t, c, "_dex_refl_set_str_User(&(unit), 0,")
}

// sliceMutFunc returns the text of a named generated function that takes a
// writable subject. sliceFunc keys off "(const", which only matches a reader.
func sliceMutFunc(c, name string) string {
	start := strings.Index(c, "_Bool "+name+"(")
	if start < 0 {
		return ""
	}
	end := strings.Index(c[start:], "\n}")
	if end < 0 {
		return c[start:]
	}
	return c[start : start+end]
}

// Every kind gets its own writer, so a program using all five finds all five.
func TestReflectEmitsEverySetterKind(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string  n: int  kw: double  live: bool }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t", n: 2, kw: 1.0, live: true }
		fmt.println(reflect.setLong(unit, 0, 5))
		fmt.println(reflect.setString(unit, 1, "x"))
		fmt.println(reflect.setInt(unit, 2, 3))
		fmt.println(reflect.setDouble(unit, 3, 2.0))
		fmt.println(reflect.setBool(unit, 4, false))
	}`)
	for _, want := range []string{
		"_dex_refl_set_long_User(&(unit), 0,",
		"_dex_refl_set_str_User(&(unit), 1,",
		"_dex_refl_set_int_User(&(unit), 2,",
		"_dex_refl_set_dbl_User(&(unit), 3,",
		"_dex_refl_set_bool_User(&(unit), 4,",
	} {
		assertContains(t, c, want)
	}
	// Only the double setter writes the double field.
	if dbl := sliceMutFunc(c, "_dex_refl_set_dbl_User"); !strings.Contains(dbl, "s->kw = v") {
		t.Errorf("the double setter does not write the double field:\n%s", dbl)
	}
	if b := sliceMutFunc(c, "_dex_refl_set_bool_User"); !strings.Contains(b, "s->live = v") {
		t.Errorf("the bool setter does not write the bool field:\n%s", b)
	}
}

// fieldTags is what lets a metadata-driven library be written without
// generics: it hands a plain function over string arrays everything it needs.
func TestReflectFieldTagsBuildsTheWholeTable(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct Row { id: long  name: string `+"`db:\"COALESCE(f.name, '')\"`"+`  note: string }
	fn main(): void {
		let shape: Row = Row {}
		let tags: string[] = reflect.fieldTags(shape, "db")
		fmt.println(tags.len())
	}`)
	// One lookup per field, in declaration order.
	for i := 0; i < 3; i++ {
		if !strings.Contains(c, fmt.Sprintf("_dex_refl_tag_Row(%d, ", i)) {
			t.Errorf("no tag lookup for field %d:\n%s", i, c)
		}
	}
	// push retains, so the minted lookup result is released after the push.
	if !strings.Contains(c, "dex_array_string_push(") || !strings.Contains(c, "dex_release(_e);") {
		t.Errorf("fieldTags must release each minted tag after pushing it:\n%s", c)
	}
}

// The key may be an expression, and a thirty-field table must not evaluate it
// thirty times.
func TestReflectFieldTagsEvaluatesKeyOnce(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	import "str"
	struct Row { a: int  b: int  d: int }
	fn main(): void {
		let shape: Row = Row {}
		let k: string = "d" + "b"
		fmt.println(reflect.fieldTags(shape, k).len())
	}`)
	// Count call sites only — the generated lookup's own definition also
	// mentions the name.
	n := 0
	for i := 0; i < 3; i++ {
		n += strings.Count(c, fmt.Sprintf("_dex_refl_tag_Row(%d, ", i))
	}
	if n != 3 {
		t.Fatalf("expected one lookup per field, got %d", n)
	}
	// The key is bound to a local first; the lookups read that local.
	if !strings.Contains(c, "const char* _dex_tmp_") {
		t.Errorf("the key must be read into a local once:\n%s", c)
	}
}

// --- db.fill ---------------------------------------------------------------

const dbFillProgram = `import "db"
	import "fmt"
	struct Person {
		id: long
		name: string
		age: int
		score: double
		active: bool
		tags: string[]
		note: string
	}
	fn main(): void {
		let conn: int = db.open("sqlite", "/tmp/x.db")
		let rows: int = db.query(conn, "SELECT * FROM people")
		let p: Person = Person { id: 0, name: "", age: 0, score: 0.0, active: false, tags: [], note: "" }
		db.fill(p, rows)
		fmt.println(p.name)
	}`

// Column i into field i, one assignment per field — straight-line, because the
// index is a constant at every position.
func TestDbFillReadsColumnPerFieldInOrder(t *testing.T) {
	c := generate(t, dbFillProgram)
	fn := sliceDbFiller(c, "Person")
	if fn == "" {
		t.Fatalf("no filler emitted:\n%s", c)
	}
	for _, want := range []string{
		"s->id = dex_db_col_long(rows, 0);",
		"dex_db_col_dexstr(rows, 1)",
		"s->age = dex_db_col_int(rows, 2);",
		"s->score = dex_db_col_double(rows, 3);",
		"s->active = dex_db_col_bool(rows, 4);",
		"dex_db_col_dexstr(rows, 6)",
	} {
		if !strings.Contains(fn, want) {
			t.Errorf("missing %q:\n%s", want, fn)
		}
	}
	assertContains(t, c, "_dex_db_fill_Person(&(p), rows)")
}

// A long must not go through the int reader, which truncates past 2^31 — an id
// column on a long-running table will reach that.
func TestDbFillAndColUseTheLongReader(t *testing.T) {
	c := generate(t, dbFillProgram)
	if strings.Contains(sliceDbFiller(c, "Person"), "s->id = dex_db_col_int") {
		t.Error("a long field read through the int column reader truncates")
	}
	c2 := generate(t, `import "db"
	import "fmt"
	fn main(): void {
		let conn: int = db.open("sqlite", "/tmp/x.db")
		let rows: int = db.query(conn, "SELECT id FROM people")
		let id: long = db.col(rows, 0)
		fmt.println(id)
	}`)
	assertContains(t, c2, "dex_db_col_long(")
}

// A field reflection cannot read is skipped, but its column position is still
// consumed so the fields after it stay aligned with the projection.
func TestDbFillSkipsNonScalarFieldButKeepsColumnAlignment(t *testing.T) {
	fn := sliceDbFiller(generate(t, dbFillProgram), "Person")
	if !strings.Contains(fn, "column 5: tags has kind array") {
		t.Errorf("the skipped column should be recorded where it was skipped:\n%s", fn)
	}
	if strings.Contains(fn, "dex_db_col_dexstr(rows, 5)") {
		t.Errorf("an array field must not be read as a string column:\n%s", fn)
	}
}

// A string column mints its reference, so the field's old one is released and
// the new one moves in — no retain, or it would leak one per row.
func TestDbFillStringColumnTransfersOwnership(t *testing.T) {
	fn := sliceDbFiller(generate(t, dbFillProgram), "Person")
	want := "{ DexString* _v = dex_db_col_dexstr(rows, 1); dex_release(s->name); s->name = _v; }"
	if !strings.Contains(fn, want) {
		t.Errorf("want %s\ngot:\n%s", want, fn)
	}
	if strings.Contains(fn, "dex_retain(_v)") {
		t.Error("the column read already minted a reference; retaining leaks one per row")
	}
}

// Nothing is emitted for a program that never opens a database.
func TestDbFillEmitsNothingWithoutTheDbModule(t *testing.T) {
	c := generate(t, `import "reflect"
	import "fmt"
	struct Person { id: long  name: string }
	fn main(): void {
		let p: Person = Person { id: 1, name: "a" }
		fmt.println(reflect.typeName(p))
	}`)
	if strings.Contains(c, "_dex_db_fill_") {
		t.Error("emitted a row filler for a program with no db module")
	}
}

func sliceDbFiller(c, structName string) string {
	start := strings.Index(c, "static void _dex_db_fill_"+structName+"(")
	if start < 0 {
		return ""
	}
	end := strings.Index(c[start:], "\n}")
	if end < 0 {
		return c[start:]
	}
	return c[start : start+end]
}

// A field the projection does not select takes no column, so the fields after
// it must not shift. Counting fields instead of columns read everything after
// such a field one position out.
func TestDbFillSkippedFieldTakesNoColumn(t *testing.T) {
	c := generate(t, `import "db"
	import "fmt"
	struct Person {
		id: long
		name: string
		derived: string `+"`db:\"-\"`"+`
		age: int
		note: string
	}
	fn main(): void {
		let conn: int = db.open("sqlite", "/tmp/x.db")
		let rows: int = db.query(conn, "SELECT id, name, age, note FROM people")
		let p: Person = Person { id: 0, name: "", derived: "", age: 0, note: "" }
		db.fill(p, rows)
		fmt.println(p.note)
	}`)
	fn := sliceDbFiller(c, "Person")
	for _, want := range []string{
		"s->id = dex_db_col_long(rows, 0);",
		"dex_db_col_dexstr(rows, 1); dex_release(s->name)",
		`derived is tagged db:"-"`,
		"s->age = dex_db_col_int(rows, 2);",
		"dex_db_col_dexstr(rows, 3); dex_release(s->note)",
	} {
		if !strings.Contains(fn, want) {
			t.Errorf("missing %q:\n%s", want, fn)
		}
	}
	if strings.Contains(fn, "s->derived") {
		t.Errorf("a field tagged db:\"-\" must not be written:\n%s", fn)
	}
}

// The db runtime's bound-parameter calls take a DexArrayString, so it does not
// compile without the array runtime — even for a program that writes no array.
func TestDbModulePullsInTheArrayRuntime(t *testing.T) {
	c := generate(t, `import "db"
	import "fmt"
	fn main(): void {
		let conn: int = db.open("sqlite", "/tmp/x.db")
		fmt.println(conn)
	}`)
	assertContains(t, c, "typedef struct {\n    DexObjHeader hdr;")
	if !strings.Contains(c, "DexArrayString") {
		t.Error("the db runtime references DexArrayString; the array runtime must be emitted with it")
	}
}

// db.fill marks the struct it fills reflected, so the tag and name lookups get
// emitted — and those call into the reflect runtime, which a program need not
// have imported.
func TestDbFillEmitsReflectRuntimeWithoutTheImport(t *testing.T) {
	c := generate(t, `import "db"
	import "fmt"
	struct Person { id: long  name: string }
	fn main(): void {
		let conn: int = db.open("sqlite", "/tmp/x.db")
		let rows: int = db.query(conn, "SELECT id, name FROM people")
		let p: Person = Person { id: 0, name: "" }
		db.fill(p, rows)
		fmt.println(p.name)
	}`)
	if strings.Contains(c, "_dex_refl_tag_Person(") && !strings.Contains(c, "static int dex_refl_tag(") {
		t.Error("emitted a tag lookup whose runtime is missing")
	}
}

// An if or while condition is generated outside any statement prelude, so a
// method whose receiver is an expression had nowhere to hoist its temporary.
// It used to fall through to the named-receiver path with an empty receiver and
// emit a call to a function that does not exist, so `if (tags[i].isEmpty())`
// would not compile while the same expression in a `let` did.
func TestExpressionReceiverInConditionKeepsItsReceiver(t *testing.T) {
	out := generate(t, `import "fmt"
fn main(): void {
    let arr: string[] = ["", "x"]
    let i: int = 0
    if (arr[i].isEmpty()) { fmt.println("e") }
    while (arr[i].len() == 0) { i = i + 1 }
}
`)
	if strings.Contains(out, "if (isEmpty())") || strings.Contains(out, " isEmpty()") {
		t.Errorf("the method lost its receiver:\n%s", out)
	}
	// Both conditions bind the receiver in a statement expression, which needs
	// no prelude and is re-evaluated correctly each iteration of the while.
	if n := strings.Count(out, "({ DexString* _dex_tmp_"); n < 2 {
		t.Errorf("expected a statement-expression receiver per condition, got %d:\n%s", n, out)
	}
	assertContains(t, out, "dex_str_isEmpty(_dex_tmp_")
	assertContains(t, out, "dex_str_len(_dex_tmp_")
}

// The inline path owns what it hoisted on the same terms the prelude path does:
// a minted receiver is released, a borrowed one is not.
func TestExpressionReceiverInConditionBalancesOwnership(t *testing.T) {
	out := generate(t, `import "fmt"
fn main(): void {
    let arr: string[] = ["", "x"]
    let sb = StringBuilder()
    if (sb.toString().len() == 0) { fmt.println("a") }
    if (arr[0].isEmpty()) { fmt.println("b") }
}
`)
	minted := lineContaining(t, out, "dex_sb_toString(sb)")
	if !strings.Contains(minted, "dex_release(_dex_tmp_") {
		t.Errorf("a minted receiver must be released:\n%s", minted)
	}
	borrowed := lineContaining(t, out, "dex_str_isEmpty(_dex_tmp_")
	if strings.Contains(borrowed, "dex_sb_toString") {
		borrowed = ""
	}
	if borrowed != "" && strings.Contains(borrowed, "dex_release") {
		t.Errorf("a borrowed receiver must not be released:\n%s", borrowed)
	}
	// A value-returning method must bind its result, not discard it.
	if strings.Contains(out, "dex_str_len(_dex_tmp_0); dex_release") {
		t.Error("the method's result was discarded instead of bound")
	}
}

// A struct read by two projections needs a filler per tag key: the fields one
// key marks "-" are not the ones the other does, so the column positions after
// a skipped field differ between them.
func TestDbFillEmitsOneFillerPerTagKey(t *testing.T) {
	out := generate(t, `import "db"
	import "fmt"
	struct Row {
		id: long
		name: string
		extra: string `+"`lean:\"-\"`"+`
		note: string
	}
	fn main(): void {
		let conn: int = db.open("sqlite", "/tmp/x.db")
		let rows: int = db.query(conn, "SELECT * FROM rows")
		let a: Row = Row {}
		db.fill(a, rows)
		let b: Row = Row {}
		db.fill(b, rows, "lean")
		fmt.println(a.note + b.note)
	}`)
	full := sliceDbFiller(out, "Row")
	lean := sliceDbFiller(out, "Row_lean")
	if full == "" || lean == "" {
		t.Fatalf("expected a filler per key:\n%s", out)
	}
	// `extra` is selected under the default key, so note is column 3.
	assertContains(t, full, "dex_db_col_dexstr(rows, 2); dex_release(s->extra)")
	assertContains(t, full, "dex_db_col_dexstr(rows, 3); dex_release(s->note)")
	// Under "lean" it is not, so note moves up to column 2.
	assertContains(t, lean, `extra is tagged lean:"-"`)
	assertContains(t, lean, "dex_db_col_dexstr(rows, 2); dex_release(s->note)")
	if strings.Contains(lean, "s->extra") {
		t.Errorf("the lean filler must not write a field its key excludes:\n%s", lean)
	}
	assertContains(t, out, "_dex_db_fill_Row(&(a), rows)")
	assertContains(t, out, "_dex_db_fill_Row_lean(&(b), rows)")
}
