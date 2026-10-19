package resolve

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func fakeRemote(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	remotes := t.TempDir()
	repo := filepath.Join(remotes, "example.com", "acme", "libs")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q", "-b", "main")
	writeFiles(t, repo, map[string]string{
		"greet/dex.mod":  "module example.com/acme/libs/greet\n",
		"greet/greet.dx": "fn hello(): string {\n    return \"v1\"\n}\n",
		"shout/dex.mod":  "module example.com/acme/libs/shout\n",
		"shout/shout.dx": "fn up(s: string): string {\n    return s\n}\n",
	})
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-q", "-m", "one")
	runGit(t, repo, "tag", "greet/v0.1.0")
	runGit(t, repo, "tag", "shout/v1.0.0")
	writeFiles(t, repo, map[string]string{
		"greet/dex.mod":  "module example.com/acme/libs/greet\n\nrequire example.com/acme/libs/shout v1.0.0\n",
		"greet/greet.dx": "fn hello(): string {\n    return \"v2\"\n}\n",
	})
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-q", "-m", "two")
	runGit(t, repo, "tag", "greet/v0.2.0")
	runGit(t, repo, "tag", "greet/v0.3.0-beta")

	saved := repoURL
	repoURL = func(r string) string { return "file://" + filepath.Join(remotes, filepath.FromSlash(r)) }
	t.Cleanup(func() { repoURL = saved })
	t.Setenv("DEX_MODCACHE", filepath.Join(t.TempDir(), "cache"))
	return repo
}

func newApp(t *testing.T) *Mod {
	t.Helper()
	dir := t.TempDir()
	file, err := InitMod(dir, "example.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := FindMod(dir)
	if err != nil || mod == nil || mod.File != file {
		t.Fatalf("FindMod: %v %v", mod, err)
	}
	return mod
}

func reread(t *testing.T, mod *Mod) *Mod {
	t.Helper()
	data, err := os.ReadFile(mod.File)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := ParseMod(mod.File, string(data))
	if err != nil {
		t.Fatalf("dex.mod no longer parses: %v\n%s", err, data)
	}
	return fresh
}

func TestGetPinnedVersion(t *testing.T) {
	fakeRemote(t)
	mod := newApp(t)
	fetched, err := Get(mod, "example.com/acme/libs/greet@v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 1 || fetched[0].Version != "v0.1.0" {
		t.Fatalf("fetched %+v", fetched)
	}
	mod = reread(t, mod)
	if mod.Require["example.com/acme/libs/greet"] != "v0.1.0" {
		t.Fatalf("require = %v", mod.Require)
	}
	file, err := importFile(mod, mod.Dir, "example.com/acme/libs/greet")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(src), "v1") {
		t.Fatalf("read %s: %q %v", file, src, err)
	}
	sums, err := ReadSums(filepath.Join(mod.Dir, SumFileName))
	if err != nil || !strings.HasPrefix(sums[sumKey("example.com/acme/libs/greet", "v0.1.0")], "h1:") {
		t.Fatalf("dex.sum = %v %v", sums, err)
	}
}

func TestGetLatestPullsInWhatTheLibraryRequires(t *testing.T) {
	fakeRemote(t)
	mod := newApp(t)
	fetched, err := Get(mod, "example.com/acme/libs/greet")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 2 || fetched[0].Version != "v0.2.0" || fetched[1].Path != "example.com/acme/libs/shout" || !fetched[1].Indirect {
		t.Fatalf("fetched %+v", fetched)
	}
	data, _ := os.ReadFile(mod.File)
	if !strings.Contains(string(data), "\n    example.com/acme/libs/shout v1.0.0 // indirect\n") {
		t.Fatalf("dex.mod:\n%s", data)
	}
	mod = reread(t, mod)
	if _, err := importFile(mod, mod.Dir, "example.com/acme/libs/shout"); err != nil {
		t.Fatal(err)
	}

	if _, err := Get(mod, "example.com/acme/libs/greet@v0.1.0"); err != nil {
		t.Fatal(err)
	}
	if reread(t, mod).Require["example.com/acme/libs/greet"] != "v0.1.0" {
		t.Fatal("a second get should move the require to the version asked for")
	}
}

func TestGetReportsWhatItCannotFind(t *testing.T) {
	fakeRemote(t)
	mod := newApp(t)
	for arg, want := range map[string]string{
		"example.com/acme/libs/greet@v9.9.9": "no release v9.9.9 — it has",
		"example.com/acme/libs/greet@main":   "not a version",
		"example.com/acme/libs/nope":         "no version tags",
		"example.com/acme/app/util":          "is this module",
		"util":                               "not a module path",
	} {
		if _, err := Get(mod, arg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Get(%q) = %v, want %q", arg, err, want)
		}
	}
}

func TestGetFindsTheModuleAboveAFile(t *testing.T) {
	fakeRemote(t)
	mod := newApp(t)
	fetched, err := Get(mod, "example.com/acme/libs/greet/internal/x@v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if fetched[0].Path != "example.com/acme/libs/greet" {
		t.Fatalf("fetched %+v", fetched)
	}
}

func TestDownloadAndVerifyCatchAnEditedCache(t *testing.T) {
	fakeRemote(t)
	mod := newApp(t)
	if _, err := Get(mod, "example.com/acme/libs/greet@v0.1.0"); err != nil {
		t.Fatal(err)
	}
	cache, _ := ModCache()
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	fetched, err := Download(reread(t, mod))
	if err != nil || len(fetched) != 1 {
		t.Fatalf("Download = %v, %v", fetched, err)
	}

	file := filepath.Join(fetched[0].Dir, "greet.dx")
	os.Chmod(file, 0o644)
	if err := os.WriteFile(file, []byte("fn hello(): string {\n    return \"evil\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(reread(t, mod)); err == nil || !strings.Contains(err.Error(), "does not match dex.sum") {
		t.Fatalf("an edited cache should fail verification, got %v", err)
	}
}

func TestSetRequireKeepsTheFileShape(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ModFileName)
	src := "module example.com/a\n\nrequire (\n    example.com/b v1.0.0\n)\n\nreplace example.com/b => ../b\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRequire(file, "example.com/b", "v1.1.0", false); err != nil {
		t.Fatal(err)
	}
	if err := SetRequire(file, "example.com/c", "v0.1.0", true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	want := "module example.com/a\n\nrequire (\n    example.com/b v1.1.0\n    example.com/c v0.1.0 // indirect\n)\n\nreplace example.com/b => ../b\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	single := filepath.Join(dir, "single.mod")
	os.WriteFile(single, []byte("module example.com/a\n"), 0o644)
	SetRequire(single, "example.com/b", "v1.0.0", false)
	SetRequire(single, "example.com/b", "v1.2.0", false)
	got, _ = os.ReadFile(single)
	if string(got) != "module example.com/a\n\nrequire example.com/b v1.2.0\n" {
		t.Fatalf("got:\n%s", got)
	}
	SetRequire(single, "example.com/c", "v0.3.0", false)
	got, _ = os.ReadFile(single)
	if string(got) != "module example.com/a\n\nrequire (\n    example.com/b v1.2.0\n    example.com/c v0.3.0\n)\n" {
		t.Fatalf("a second require should fold into a block, got:\n%s", got)
	}
	if _, err := ParseMod(single, string(got)); err != nil {
		t.Fatal(err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.10.0", "v1.9.0", 1},
		{"v0.1.0", "v0.2.0", -1},
		{"v1.0.0-beta", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if got := latestVersion([]string{"v0.1.0", "v0.3.0-beta", "v0.2.0"}); got != "v0.2.0" {
		t.Errorf("latest = %s", got)
	}
	if got := latestVersion([]string{"v0.3.0-beta"}); got != "v0.3.0-beta" {
		t.Errorf("latest with only a prerelease = %s", got)
	}
}

func TestCheckModuleImportsNamesTheImport(t *testing.T) {
	t.Setenv("DEX_MODCACHE", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ModFileName), []byte("module example.com/app\n\nrequire example.com/lib v1.0.0\n"), 0o644)
	refs := []ImportRef{{Path: "fmt", Line: 1, Col: 1}, {Path: "example.com/lib", Line: 2, Col: 1}}
	err := CheckModuleImports(refs, dir, "main.dx")
	if err == nil || !strings.Contains(err.Error(), "main.dx:2:1:") || !strings.Contains(err.Error(), "dex mod download") {
		t.Fatalf("got %v", err)
	}
	if err := CheckModuleImports([]ImportRef{{Path: "util"}}, t.TempDir(), "main.dx"); err != nil {
		t.Fatalf("a relative import needs no dex.mod: %v", err)
	}
}
