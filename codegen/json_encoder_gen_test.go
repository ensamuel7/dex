package codegen

import "testing"

func TestCodegenJsonEncodeUsesGeneratedEncoder(t *testing.T) {
	out := generate(t, `import "json"
struct Post {
    id: long
    body: string
    tags: string[]
}
struct Page {
    posts: Post[]
}
fn main(): void {
    let p: Page = Page { posts: [] }
    let text: string = json.encode(p)
}`)
	assertContains(t, out, "static void dex_jenc_Dex_Page(DexJB* b, const Dex_Page* v)")
	assertContains(t, out, "static void dex_jenc_Dex_Post(DexJB* b, const Dex_Post* v)")
	assertContains(t, out, `dex_jb_raw(b, "{\"id\":", 6);`)
	assertNotContains(t, out, "dex_json_encode_struct(&p")
}

func TestCodegenJsonEncodeFallsBackForUnsupportedFields(t *testing.T) {
	out := generate(t, `import "json"
struct Holder {
    name: string
    scores: map[string, int]
}
fn main(): void {
    let h: Holder = Holder { name: "a", scores: {} }
    let text: string = json.encode(h)
}`)
	assertNotContains(t, out, "dex_jenc_Dex_Holder")
	assertContains(t, out, "dex_json_encode_struct(")
}
