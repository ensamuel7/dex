package checker

import "testing"

func TestRemovedJsonFunctionsRejected(t *testing.T) {
	for _, name := range []string{"new", "set", "setObj", "setArray", "get", "getInt",
		"getBool", "getDouble", "getLong", "arrayNew", "arrayLen", "arrayGet",
		"arrayGetRaw", "arrayPush", "arrayPushObj"} {
		mustFail(t, `import "json"
		fn main(): void {
			let d: string = "{}"
			let r = json.`+name+`(d, "k", "v")
		}`, "undefined function '"+name+"' in module 'json'")
	}
}

func TestJsonEncodeDecodeSurvive(t *testing.T) {
	mustCheck(t, `import "json"
	struct P { name: string }
	fn main(): void {
		let v: json.Value = { name: "a" }
		let s: string = json.encode(v)
		let p: P = json.decode(s)
	}`)
}
