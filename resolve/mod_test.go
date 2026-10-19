package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsModulePath(t *testing.T) {
	for path, want := range map[string]bool{
		"github.com/acme/greet":       true,
		"github.com/acme/greet/inner": true,
		"example.org":                 true,
		"service/users":               false,
		"util":                        false,
		"../wire/frame":               false,
		"./local":                     false,
		"modules/sub/tally":           false,
	} {
		if got := IsModulePath(path); got != want {
			t.Errorf("IsModulePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseMod(t *testing.T) {
	mod, err := ParseMod("/proj/dex.mod", `// the app
module github.com/acme/web

require github.com/acme/greet v0.1.0
require (
    github.com/acme/kit v1.2.3 // shared helpers
    github.com/acme/kit/v2 v2.0.0-beta.1
)
replace github.com/acme/greet => ../greet
replace (
    github.com/acme/kit => /abs/kit
)
`)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Path != "github.com/acme/web" || mod.Dir != "/proj" {
		t.Errorf("path %q dir %q", mod.Path, mod.Dir)
	}
	if mod.Require["github.com/acme/kit/v2"] != "v2.0.0-beta.1" || len(mod.Require) != 3 {
		t.Errorf("require = %v", mod.Require)
	}
	if mod.Replace["github.com/acme/greet"] != "/greet" || mod.Replace["github.com/acme/kit"] != "/abs/kit" {
		t.Errorf("replace = %v", mod.Replace)
	}
}

func TestParseModErrors(t *testing.T) {
	for src, want := range map[string]string{
		"require github.com/a/b v1.0.0\n":                                      "no module line",
		"module x.com/a\nmodule x.com/b\n":                                     "declared twice",
		"module x.com/a\nrequire x.com/b latest\n":                             "not a version",
		"module x.com/a\nrequire x.com/b v1.0.0\nrequire x.com/b v1.1.0\n":     "required twice",
		"module x.com/a\nreplace x.com/b => ../b\n":                            "replaced but not required",
		"module x.com/a\nrequire x.com/b v1.0.0\nreplace x.com/b => x.com/c\n": "local directory",
		"module x.com/a\nrequire x.com/b v1.0.0\nreplace x.com/b ../b\n":       "expected `replace",
		"module x.com/a\nexclude x.com/b v1.0.0\n":                             "unknown directive",
		"module x.com/a\nrequire (\n x.com/b v1.0.0\n":                         "never closed",
		"module not a path\n":                                                  "expected `module",
	} {
		_, err := ParseMod("/p/dex.mod", src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("for %q got %v, want an error containing %q", src, err, want)
		}
	}
}

func TestImportFile(t *testing.T) {
	mod := &Mod{
		File:    "/app/dex.mod",
		Dir:     "/app",
		Path:    "github.com/acme/web",
		Require: map[string]string{"github.com/acme/greet": "v0.1.0", "github.com/acme/far": "v1.0.0"},
		Replace: map[string]string{"github.com/acme/greet": "/libs/greet"},
	}
	for path, want := range map[string]string{
		"service/users":                        "/app/src/service/users.dx",
		"github.com/acme/web/service/users":    "/app/service/users.dx",
		"github.com/acme/greet":                "/libs/greet/greet.dx",
		"github.com/acme/greet/internal/shout": "/libs/greet/internal/shout.dx",
	} {
		got, err := importFile(mod, "/app/src", path)
		if err != nil || got != filepath.FromSlash(want) {
			t.Errorf("importFile(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
	for path, want := range map[string]string{
		"github.com/acme/far":  "required but not downloaded",
		"github.com/other/lib": "in no module",
	} {
		if _, err := importFile(mod, "/app", path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("importFile(%q) error = %v, want %q", path, err, want)
		}
	}
	if _, err := importFile(nil, "/app", "github.com/acme/greet"); err == nil || !strings.Contains(err.Error(), "dex mod init") {
		t.Errorf("no dex.mod: got %v", err)
	}
	if got, err := importFile(nil, "/app", "util"); err != nil || got != filepath.FromSlash("/app/util.dx") {
		t.Errorf("a relative import needs no dex.mod: got %q, %v", got, err)
	}
}

// The longest module path wins, so a major version's own path is not read
// from the module it extends.
func TestImportFilePrefersLongestModulePath(t *testing.T) {
	mod := &Mod{
		Dir:     "/app",
		Path:    "github.com/acme/web",
		Require: map[string]string{"github.com/acme/kit": "v1.0.0", "github.com/acme/kit/v2": "v2.0.0"},
		Replace: map[string]string{"github.com/acme/kit": "/kit1", "github.com/acme/kit/v2": "/kit2"},
	}
	got, err := importFile(mod, "/app", "github.com/acme/kit/v2/strings")
	if err != nil || got != filepath.FromSlash("/kit2/strings.dx") {
		t.Errorf("got %q, %v; want /kit2/strings.dx", got, err)
	}
}

func TestFindModWalksUpAndInitMod(t *testing.T) {
	dir := t.TempDir()
	if mod, err := FindMod(dir); err != nil || mod != nil {
		t.Fatalf("no dex.mod should be nil, nil; got %v, %v", mod, err)
	}
	file, err := InitMod(dir, "github.com/acme/web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InitMod(dir, "github.com/acme/web"); err == nil {
		t.Error("a second init should refuse to overwrite")
	}
	if _, err := InitMod(dir, "not a path"); err == nil {
		t.Error("init should refuse a malformed path")
	}
	nested := filepath.Join(dir, "service", "users")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mod, err := FindMod(nested)
	if err != nil || mod == nil || mod.File != file || mod.Path != "github.com/acme/web" {
		t.Fatalf("FindMod from a subdirectory: %+v, %v", mod, err)
	}
}

// A library replaced by a sibling directory, importing its own internal file by
// module path, used from an app importing its own code by module path.
func TestResolveThroughDexMod(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"dex.mod": `module github.com/acme/web
require github.com/acme/greet v0.1.0
replace github.com/acme/greet => ./vendor/greet
`,
		"main.dx": `import "github.com/acme/greet"
import "github.com/acme/web/service/users"
fn main(): void {}
`,
		"service/users.dx": `fn name(): string { return "Ama" }
`,
		"vendor/greet/greet.dx": `import "github.com/acme/greet/internal/shout"
fn hello(who: string): string { return shout.loud(who) }
`,
		"vendor/greet/internal/shout.dx": `fn loud(s: string): string { return s }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, fn := range program.Functions {
		names[fn.Name] = true
	}
	for _, want := range []string{"greet_hello", "shout_loud", "users_name"} {
		if !names[want] {
			t.Errorf("no %s; have %v", want, names)
		}
	}
}

func TestResolveReportsUnplacedModuleAtTheImport(t *testing.T) {
	_, err := resolveProject(t, map[string]string{
		"dex.mod": "module github.com/acme/web\n",
		"main.dx": `import "github.com/acme/missing"
fn main(): void {}
`,
	})
	if err == nil || !strings.Contains(err.Error(), "main.dx:1:") || !strings.Contains(err.Error(), "in no module") {
		t.Fatalf("got %v", err)
	}
}

// The language server's entry point finds the dex.mod from the program's
// directory, wherever the importing file is.
func TestImportFileFindsTheModFromTheProgram(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ModFileName), []byte("module github.com/acme/web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ImportFile(dir, filepath.Join(dir, "deep", "er"), "github.com/acme/web/service/users")
	if err != nil || got != filepath.Join(dir, "service", "users.dx") {
		t.Errorf("got %q, %v", got, err)
	}
}
