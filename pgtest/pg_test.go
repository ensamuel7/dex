package pgtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, name string, env ...string) {
	dir := t.TempDir()
	dex := filepath.Join(dir, "dex")
	if out, err := exec.Command("go", "build", "-o", dex, "..").CombinedOutput(); err != nil {
		t.Fatalf("build dex: %v\n%s", err, out)
	}
	src, _ := filepath.Abs(filepath.Join("testdata", name+".dx"))
	build := exec.Command(dex, "build", "--no-lint", src)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, out)
	}
	cmd := exec.Command(filepath.Join(dir, "build", name))
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "ok") {
		t.Fatalf("%s: %v\n%s", name, err, out)
	}
}

func TestPostgres(t *testing.T) {
	dsn := os.Getenv("DEX_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set DEX_TEST_POSTGRES to a database this test may create tables in")
	}
	run(t, "pg", "DATABASE_URL="+dsn, "DB_POOL_SIZE=1")
}

func TestBcrypt(t *testing.T) {
	if exec.Command("pkg-config", "--exists", "libxcrypt").Run() != nil {
		t.Skip("bcrypt needs libxcrypt")
	}
	run(t, "bcrypt")
}
