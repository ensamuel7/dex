package stdlib

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

type pkgResult struct {
	flags []string
	err   error
}

var (
	pkgMu    sync.Mutex
	pkgCache = map[string]pkgResult{}

	pkgDefineRe = regexp.MustCompile(`^-D[A-Za-z_][A-Za-z0-9_]*(=[A-Za-z0-9_.]*)?$`)
	pkgNameRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+@-]*$`)
)

func ValidPkgName(name string) bool {
	return pkgNameRe.MatchString(name)
}

func PkgConfig(name string) ([]string, error) {
	pkgMu.Lock()
	defer pkgMu.Unlock()
	if r, ok := pkgCache[name]; ok {
		return r.flags, r.err
	}
	flags, err := runPkgConfig(name)
	pkgCache[name] = pkgResult{flags, err}
	return flags, err
}

func runPkgConfig(name string) ([]string, error) {
	if _, err := exec.LookPath("pkg-config"); err != nil {
		return nil, fmt.Errorf("pkg %q needs pkg-config, which is not installed", name)
	}
	out, err := exec.Command("pkg-config", "--cflags", "--libs", "--", name).Output()
	if err != nil {
		return nil, fmt.Errorf("pkg-config does not know %q — install the library's development package, or check the name with `pkg-config --list-all`", name)
	}
	return filterPkgFlags(name, strings.Fields(string(out)))
}

func filterPkgFlags(name string, fields []string) ([]string, error) {
	var flags []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "-pthread":
		case strings.HasPrefix(f, "-I"), strings.HasPrefix(f, "-L"):
			if len(f) == 2 {
				return nil, fmt.Errorf("pkg-config for %q printed %s with its path as a separate word, which is not passed on", name, f)
			}
		case strings.HasPrefix(f, "-l") && len(f) > 2:
		case pkgDefineRe.MatchString(f):
		case f == "-framework" && i+1 < len(fields) && pkgNameRe.MatchString(fields[i+1]):
			flags = append(flags, f, fields[i+1])
			i++
			continue
		default:
			return nil, fmt.Errorf("pkg-config for %q printed %q, which an external block does not pass to the compiler — only -I, -L, -l, -D, -pthread and -framework are", name, f)
		}
		flags = append(flags, f)
	}
	return flags, nil
}
