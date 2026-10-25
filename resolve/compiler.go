package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// compilerVersion is the running compiler's version, as `dex version` prints it
// without the prefix: "0.9.14" for a release, "0.9.14-dev" for `make build`,
// "dev" for a plain `go build`. main sets it at startup; resolve cannot import
// main, and a test sets it directly.
var compilerVersion = "dev"

// SetCompilerVersion tells the resolver which compiler is running, so a module
// whose dex.mod needs a newer one is refused at the import that reaches it.
func SetCompilerVersion(v string) {
	compilerVersion = v
}

// dexVersionRe is the form of a `dex` directive's version: the release tag
// without its v, and without a pre-release suffix — a module needs a release.
var dexVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// compilerSatisfies reports whether a compiler of version running can build a
// module that needs version needed.
//
// A -suffix on the running version is ignored: "0.9.14-dev" is a build of the
// 0.9.14 tree or later, and satisfies 0.9.14. A compiler built straight from a
// checkout reports "dev" and is never refused, the way `dex update` already
// treats it — whoever runs one is working on the compiler itself.
func compilerSatisfies(running, needed string) bool {
	if needed == "" || running == "dev" || running == "" {
		return true
	}
	core, _, _ := strings.Cut(running, "-")
	return CompareVersions(core, needed) >= 0
}

// CompilerTooOld describes a module that needs a newer compiler than this one,
// or returns nil. who names the module, as it is imported.
func CompilerTooOld(who, needed string) error {
	if compilerSatisfies(compilerVersion, needed) {
		return nil
	}
	return fmt.Errorf("%s needs dex %s or later; this is dex %s\n    upgrade with `dex update`, or `make install` in a compiler checkout",
		who, needed, compilerVersion)
}

// checkModuleCompiler refuses a module whose own dex.mod, in its root
// directory, needs a newer compiler. It runs before any of the module's files
// are read, because the alternative is a compile error from inside the
// package, pointing at code that is correct, which reads as a bug in the
// package rather than an old compiler.
//
// A module with no dex.mod needs nothing. One whose dex.mod does not parse is
// not refused here: that error belongs to whoever reads the file for its own
// sake, and reporting it here would turn every import into a parse report.
func checkModuleCompiler(root, modulePath string) error {
	data, err := os.ReadFile(filepath.Join(root, ModFileName))
	if err != nil {
		return nil
	}
	own, err := ParseMod(filepath.Join(root, ModFileName), string(data))
	if err != nil {
		return nil
	}
	return CompilerTooOld(modulePath, own.Dex)
}

// CheckMainCompiler refuses a program whose own dex.mod needs a newer compiler,
// naming the directive's line. A program with no dex.mod, or one that does not
// parse, is left to the checks that already handle those.
func CheckMainCompiler(sourceDir string) error {
	mod, err := FindMod(sourceDir)
	if err != nil || mod == nil {
		return nil
	}
	if tooOld := CompilerTooOld(mod.Path, mod.Dex); tooOld != nil {
		return fmt.Errorf("%s:%d: %v", mod.File, mod.DexLine, tooOld)
	}
	return nil
}
