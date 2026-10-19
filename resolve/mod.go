package resolve

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const ModFileName = "dex.mod"

type Mod struct {
	File    string
	Dir     string
	Path    string
	Require map[string]string
	Replace map[string]string
}

var (
	modPathRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*(/[A-Za-z0-9._~-]+)*$`)
	versionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
)

func IsModulePath(path string) bool {
	first := path
	if i := strings.Index(path, "/"); i >= 0 {
		first = path[:i]
	}
	return first != "." && first != ".." && strings.Contains(first, ".")
}

func FindMod(dir string) (*Mod, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		file := filepath.Join(abs, ModFileName)
		if src, err := os.ReadFile(file); err == nil {
			return ParseMod(file, string(src))
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, nil
		}
		abs = parent
	}
}

func ParseMod(file, src string) (*Mod, error) {
	mod := &Mod{
		File:    file,
		Dir:     filepath.Dir(file),
		Require: map[string]string{},
		Replace: map[string]string{},
	}
	errAt := func(line int, format string, args ...any) error {
		return fmt.Errorf("%s:%d: %s", file, line, fmt.Sprintf(format, args...))
	}

	block := ""
	scanner := bufio.NewScanner(strings.NewReader(src))
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		verb := block
		if block == "" {
			verb = fields[0]
			fields = fields[1:]
			if len(fields) == 1 && fields[0] == "(" && (verb == "require" || verb == "replace") {
				block = verb
				continue
			}
		} else if fields[0] == ")" && len(fields) == 1 {
			block = ""
			continue
		}

		switch verb {
		case "module":
			if mod.Path != "" {
				return nil, errAt(n, "module is declared twice")
			}
			if len(fields) != 1 || !modPathRe.MatchString(fields[0]) {
				return nil, errAt(n, "expected `module <path>`, as in module github.com/acme/app")
			}
			mod.Path = fields[0]

		case "require":
			if len(fields) != 2 {
				return nil, errAt(n, "expected `require <module> <version>`, as in require github.com/acme/dexsqlite v1.0.0")
			}
			path, version := fields[0], fields[1]
			if !modPathRe.MatchString(path) {
				return nil, errAt(n, "%q is not a module path", path)
			}
			if !versionRe.MatchString(version) {
				return nil, errAt(n, "%q is not a version — versions are git tags like v1.2.0", version)
			}
			if _, dup := mod.Require[path]; dup {
				return nil, errAt(n, "%s is required twice", path)
			}
			mod.Require[path] = version

		case "replace":
			if len(fields) != 3 || fields[1] != "=>" {
				return nil, errAt(n, "expected `replace <module> => <directory>`, as in replace github.com/acme/dexsqlite => ../dexsqlite")
			}
			path, target := fields[0], fields[2]
			if !modPathRe.MatchString(path) {
				return nil, errAt(n, "%q is not a module path", path)
			}
			if _, dup := mod.Replace[path]; dup {
				return nil, errAt(n, "%s is replaced twice", path)
			}
			if !strings.HasPrefix(target, ".") && !filepath.IsAbs(target) {
				return nil, errAt(n, "a replace names a local directory, written ./dir or ../dir — %q is neither", target)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(mod.Dir, target)
			}
			mod.Replace[path] = filepath.Clean(target)

		default:
			return nil, errAt(n, "unknown directive %q — a dex.mod has module, require and replace", verb)
		}
	}
	if block != "" {
		return nil, fmt.Errorf("%s: the %s ( block is never closed", file, block)
	}
	if mod.Path == "" {
		return nil, fmt.Errorf("%s: no module line — the first line is `module <path>`, as in module github.com/acme/app", file)
	}
	for path := range mod.Replace {
		if _, ok := mod.Require[path]; !ok {
			return nil, fmt.Errorf("%s: %s is replaced but not required — add `require %s v0.0.0` (any version, while it is replaced)", file, path, path)
		}
	}
	return mod, nil
}

func importFile(mod *Mod, fromDir, path string) (string, error) {
	if !IsModulePath(path) {
		return filepath.Join(fromDir, path+".dx"), nil
	}
	if mod == nil {
		return "", fmt.Errorf("import %q names a module, and there is no %s here or above — create one with: dex mod init <module path>", path, ModFileName)
	}

	root, modulePath := "", ""
	if within(path, mod.Path) {
		root, modulePath = mod.Dir, mod.Path
	} else if m := longestPrefix(path, mod.Replace); m != "" {
		root, modulePath = mod.Replace[m], m
	} else if m := longestPrefix(path, mod.Require); m != "" {
		return "", fmt.Errorf("import %q is in %s %s, which is required but not downloaded — fetching arrives with dex get; until then point it at a local copy in %s: replace %s => ../path/to/it",
			path, m, mod.Require[m], mod.File, m)
	} else {
		return "", fmt.Errorf("import %q is in no module %s requires — add the module that provides it: require <module> <version>", path, mod.File)
	}

	rest := strings.TrimPrefix(strings.TrimPrefix(path, modulePath), "/")
	if rest == "" {
		rest = filepath.Base(modulePath)
	}
	return filepath.Join(root, filepath.FromSlash(rest)+".dx"), nil
}

func ImportFile(sourceDir, fromDir, path string) (string, error) {
	mod, err := FindMod(sourceDir)
	if err != nil {
		return "", err
	}
	return importFile(mod, fromDir, path)
}

func within(path, module string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}

func longestPrefix(path string, m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		if within(path, k) {
			return k
		}
	}
	return ""
}

func InitMod(dir, path string) (string, error) {
	if !modPathRe.MatchString(path) {
		return "", fmt.Errorf("%q is not a module path — a module is named by its URL, as in github.com/acme/app", path)
	}
	file := filepath.Join(dir, ModFileName)
	if _, err := os.Stat(file); err == nil {
		return "", fmt.Errorf("%s already exists", file)
	}
	if err := os.WriteFile(file, []byte("module "+path+"\n"), 0o644); err != nil {
		return "", err
	}
	return file, nil
}
