package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withCompiler runs the rest of a test as compiler version v.
func withCompiler(t *testing.T, v string) {
	t.Helper()
	saved := compilerVersion
	SetCompilerVersion(v)
	t.Cleanup(func() { compilerVersion = saved })
}

func TestCompilerSatisfies(t *testing.T) {
	for _, c := range []struct {
		running, needed string
		want            bool
	}{
		{"0.9.14", "0.9.14", true},      // equal
		{"0.9.7", "0.9.14", false},      // older, and 7 < 14 numerically, not as text
		{"0.10.0", "0.9.14", true},      // newer
		{"1.0.0", "0.9.14", true},       // newer major
		{"0.9.14-dev", "0.9.14", true},  // a -dev build of the release it needs
		{"0.9.13-dev", "0.9.14", false}, // a suffix does not lift an older build
		{"0.9.7-dev", "0.9.14", false},  // the case that motivated this
		{"dev", "99.0.0", true},         // a plain go build is never refused
		{"0.9.7", "", true},             // no requirement
	} {
		if got := compilerSatisfies(c.running, c.needed); got != c.want {
			t.Errorf("compilerSatisfies(%q, %q) = %v, want %v", c.running, c.needed, got, c.want)
		}
	}
}

// A required module that needs a newer compiler is refused at the import that
// reaches it, with an error that says so, rather than with whatever error its
// source produces on an old compiler. The module's .dx here is not Dex at all,
// so any read of it would fail differently.
func TestRequiredModuleNeedingANewerCompilerFailsAtTheImport(t *testing.T) {
	withCompiler(t, "0.9.7-dev")
	t.Setenv("DEX_MODCACHE", t.TempDir())
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		ModFileName:          "module example.com/app\n\nrequire example.com/lib v0.1.0\nreplace example.com/lib => ./lib\n",
		"lib/" + ModFileName: "module example.com/lib\n\ndex 0.9.14\n",
		"lib/lib.dx":         "this is not dex {{{\n",
	})
	refs := []ImportRef{{Path: "fmt", Line: 1, Col: 1}, {Path: "example.com/lib", Line: 8, Col: 1}}
	err := CheckModuleImports(refs, dir, "main.dx")
	if err == nil {
		t.Fatal("a module needing dex 0.9.14 was accepted by 0.9.7-dev")
	}
	for _, want := range []string{"main.dx:8:1:", "example.com/lib needs dex 0.9.14 or later; this is dex 0.9.7-dev", "dex update"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}

	withCompiler(t, "0.9.14")
	if err := CheckModuleImports(refs, dir, "main.dx"); err != nil {
		t.Errorf("the compiler it needs was refused: %v", err)
	}
}

// The check also stops an import one module down, at the import in the module
// that reached it, before the deeper module's source is parsed.
func TestATransitiveModuleNeedingANewerCompilerIsNeverParsed(t *testing.T) {
	withCompiler(t, "0.9.7")
	t.Setenv("DEX_MODCACHE", t.TempDir())
	_, err := resolveProject(t, map[string]string{
		ModFileName: `module example.com/app

require (
    example.com/mid v0.1.0
    example.com/deep v0.1.0
)
replace (
    example.com/mid => ./mid
    example.com/deep => ./deep
)
`,
		"main.dx":             "import \"example.com/mid\"\n\nfn main(): void {\n    mid.hello()\n}\n",
		"mid/" + ModFileName:  "module example.com/mid\n",
		"mid/mid.dx":          "import \"example.com/deep\"\n\nfn hello(): void {\n    deep.hello()\n}\n",
		"deep/" + ModFileName: "module example.com/deep\n\ndex 0.9.14\n",
		"deep/deep.dx":        "this is not dex {{{\n",
	})
	if err == nil {
		t.Fatal("a module needing dex 0.9.14 was resolved by 0.9.7")
	}
	if !strings.Contains(err.Error(), "example.com/deep needs dex 0.9.14 or later") {
		t.Errorf("got %v, want the compiler-version error rather than one from deep.dx", err)
	}
	if !strings.Contains(err.Error(), "mid.dx:1:") {
		t.Errorf("got %v, want it positioned at mid.dx's import", err)
	}
}

func TestTheProgramsOwnModuleIsChecked(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{ModFileName: "module example.com/app\n\ndex 0.9.14\n"})

	withCompiler(t, "0.9.7")
	err := CheckModuleImports(nil, dir, "main.dx")
	if err == nil || !strings.Contains(err.Error(), ModFileName+":3:") || !strings.Contains(err.Error(), "needs dex 0.9.14") {
		t.Errorf("got %v, want an error at the dex line of dex.mod", err)
	}

	withCompiler(t, "dev")
	if err := CheckModuleImports(nil, dir, "main.dx"); err != nil {
		t.Errorf("a dev compiler was refused: %v", err)
	}
}

func TestModInitWritesTheCompilerVersion(t *testing.T) {
	for running, want := range map[string]string{
		"0.9.14":     "module example.com/app\n\ndex 0.9.14\n",
		"0.9.14-dev": "module example.com/app\n\ndex 0.9.14\n",
		"dev":        "module example.com/app\n",
	} {
		withCompiler(t, running)
		dir := t.TempDir()
		file, err := InitMod(dir, "example.com/app")
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(file)
		if string(got) != want {
			t.Errorf("as %s, init wrote %q, want %q", running, got, want)
		}
		if _, err := FindMod(filepath.Dir(file)); err != nil {
			t.Errorf("as %s, init wrote a dex.mod that does not parse: %v", running, err)
		}
	}
}

// dex get still fetches a module that needs a newer compiler, since that may be
// what the user is upgrading for, but it says so now rather than at the build.
func TestGetMarksAModuleNeedingANewerCompiler(t *testing.T) {
	withCompiler(t, "0.9.7")
	repo := fakeRemote(t)
	writeFiles(t, repo, map[string]string{"greet/dex.mod": "module example.com/acme/libs/greet\n\ndex 0.9.14\n"})
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-qm", "needs 0.9.14")
	runGit(t, repo, "tag", "greet/v0.4.0")

	mod := newApp(t)
	fetched, err := Get(mod, "example.com/acme/libs/greet@v0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) == 0 || fetched[0].NeedsDex != "0.9.14" {
		t.Fatalf("fetched %+v, want the module marked as needing dex 0.9.14", fetched)
	}

	withCompiler(t, "0.9.14")
	mod = newApp(t)
	fetched, err = Get(mod, "example.com/acme/libs/greet@v0.4.0")
	if err != nil || fetched[0].NeedsDex != "" {
		t.Errorf("a satisfied requirement was marked: %+v, %v", fetched, err)
	}
}
