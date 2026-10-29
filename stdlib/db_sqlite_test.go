package stdlib

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestSqliteFlagsDefaultLinksSystemLibrary(t *testing.T) {
	got, err := sqliteFlags("")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"-lsqlite3"}) {
		t.Errorf("got %v", got)
	}
}

func TestSqliteFlagsBundlesAmalgamation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sqlite3.c")
	os.WriteFile(src, []byte{}, 0644)
	os.WriteFile(filepath.Join(dir, "sqlite3.h"), []byte{}, 0644)
	got, err := sqliteFlags(src)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "-lsqlite3") {
		t.Errorf("still links the system library: %v", got)
	}
	if got[0] != "-I"+dir {
		t.Errorf("sqlite3.h from the amalgamation must come first: %v", got)
	}
	for _, want := range []string{src, "-DSQLITE_THREADSAFE=1", "-DSQLITE_SECURE_DELETE", "-DSQLITE_ENABLE_FTS5"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s: %v", want, got)
		}
	}
}

func TestSqliteFlagsRejectsWhatIsNotAnAmalgamation(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other.c")
	os.WriteFile(other, []byte{}, 0644)
	noHeader := filepath.Join(t.TempDir(), "sqlite3.c")
	os.WriteFile(noHeader, []byte{}, 0644)
	for _, src := range []string{other, filepath.Join(dir, "sqlite3.c"), noHeader} {
		if _, err := sqliteFlags(src); err == nil {
			t.Errorf("%s should be refused", src)
		}
	}
}
