package stdlib

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
)

func TestValidPkgName(t *testing.T) {
	for _, ok := range []string{"openssl", "libpq", "gtk+-3.0", "sqlite3", "python3.12-embed"} {
		if !ValidPkgName(ok) {
			t.Errorf("%q should be a valid pkg name", ok)
		}
	}
	// A leading '-' would be read by pkg-config as one of its own options.
	for _, bad := range []string{"", "--variable=prefix", "-lz", "a b", "x;y", "$(id)"} {
		if ValidPkgName(bad) {
			t.Errorf("%q should not be a valid pkg name", bad)
		}
	}
}

func TestFilterPkgFlagsKeepsWhatALibraryNeeds(t *testing.T) {
	in := strings.Fields("-I/opt/homebrew/include -D_THREAD_SAFE -DLEVEL=2 -pthread -L/opt/homebrew/lib -lssl -lcrypto -framework CoreFoundation")
	got, err := filterPkgFlags("x", in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("got %v, want %v", got, in)
	}
}

// pkg-config output reaches the compiler, so anything beyond what a library
// needs is refused rather than passed through.
func TestFilterPkgFlagsRefusesTheRest(t *testing.T) {
	for _, out := range []string{
		"-I/inc -fplugin=/tmp/evil.so",
		"-Wl,-rpath,/tmp",
		"-DX=$(id)",
		"-o /tmp/out",
		"-I /split/path",
		"-framework",
	} {
		if _, err := filterPkgFlags("x", strings.Fields(out)); err == nil {
			t.Errorf("%q should be refused", out)
		}
	}
}

// CFlags gains the pkg-config flags by append, so sharing Links' backing array
// would write them into the block's own declaration.
func TestFromExternalDoesNotWriteIntoLinks(t *testing.T) {
	if _, err := PkgConfig("zlib"); err != nil {
		t.Skip("pkg-config has no zlib here")
	}
	links := make([]string, 1, 4)
	links[0] = "-lz"
	FromExternal(ast.ExternalModule{Name: "z", Links: links, Packages: []string{"zlib"}})
	if spare := links[:2][1]; spare != "" {
		t.Errorf("pkg-config flags were written into Links' backing array: %q", spare)
	}
}
