package codegen

import "testing"

// json.encode of a returned struct binds it before taking its address, and
// releases what it owns.
func TestCodegenJsonEncodeCallBindsTemporary(t *testing.T) {
	out := generate(t, `import "json"
struct Item {
    name: string
}
fn make(): Item {
    return Item { name: "a" + "b" }
}
fn main(): void {
    let text: string = json.encode(make())
}`)
	assertNotContains(t, out, "dex_json_encode_struct(&make()")
	assertContains(t, out, "Dex_Item _dex_tmp_")
}

// Each entry of a map encoded to JSON frees the text built before it.
func TestCodegenJsonMapFreesIntermediate(t *testing.T) {
	out := generate(t, `import "json"
fn main(): void {
    let m: map[string, string] = {}
    m.set("a", "b")
    let text: string = json.encode(m)
}`)
	assertContains(t, out, "if (_n != _r) free((void*)_r);")
}

// Structs are forward-declared ahead of the interface vtables that name them.
func TestCodegenStructsForwardDeclaredBeforeInterfaces(t *testing.T) {
	out := generate(t, `struct Job {
    size: int
}
interface Runner {
    fn run(job: Job): Job
}
fn main(): void {
}`)
	fwd := indexOf(out, "typedef struct Dex_Job Dex_Job;")
	iface := indexOf(out, "} Dex_Runner;")
	def := indexOf(out, "struct Dex_Job {")
	if fwd < 0 || iface < 0 || def < 0 {
		t.Fatal("expected the forward declaration, the interface and the definition")
	}
	if !(fwd < iface && iface < def) {
		t.Error("want forward declaration, then interface, then struct definition")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// A string variable's isEmpty() is a bool in an object literal, as a field's is.
func TestCodegenStringVarIsEmptyInObjectLiteral(t *testing.T) {
	out := generate(t, `import "json"
fn main(): void {
    let s: string = ""
    let text: string = json.encode({ empty: s.isEmpty() })
}`)
	assertContains(t, out, `dex_jv_put_cstr(_jo, "empty", dex_jv_bool(`)
}
