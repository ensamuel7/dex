package resolve

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const SumFileName = "dex.sum"

func ModCache() (string, error) {
	if dir := os.Getenv("DEX_MODCACHE"); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home directory for the module cache — set DEX_MODCACHE: %v", err)
	}
	return filepath.Join(home, ".dex", "pkg", "mod"), nil
}

func ModuleDir(cache, path, version string) string {
	return filepath.Join(cache, filepath.FromSlash(path)+"@"+version)
}

var repoURL = func(repo string) string {
	return "https://" + repo + ".git"
}

var knownHosts = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
	"codeberg.org":  true,
}

type source struct {
	repo   string
	url    string
	subdir string
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

func findRepo(path string) (source, error) {
	segs := strings.Split(path, "/")
	if knownHosts[segs[0]] {
		if len(segs) < 3 {
			return source{}, fmt.Errorf("%q names no repository — a %s module path is %s/<owner>/<repo>", path, segs[0], segs[0])
		}
		repo := strings.Join(segs[:3], "/")
		return source{repo: repo, url: repoURL(repo), subdir: strings.Join(segs[3:], "/")}, nil
	}
	for n := 2; n <= len(segs); n++ {
		repo := strings.Join(segs[:n], "/")
		if _, err := git("ls-remote", "--heads", repoURL(repo)); err == nil {
			return source{repo: repo, url: repoURL(repo), subdir: strings.Join(segs[n:], "/")}, nil
		}
	}
	return source{}, fmt.Errorf("no git repository found for %q", path)
}

func tagFor(subdir, version string) string {
	if subdir == "" {
		return version
	}
	return subdir + "/" + version
}

func remoteTags(url string) ([]string, error) {
	out, err := git("ls-remote", "--tags", "--refs", url)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "refs/tags/"); i >= 0 {
			tags = append(tags, strings.TrimSpace(line[i+len("refs/tags/"):]))
		}
	}
	return tags, nil
}

func versionsIn(tags []string, subdir string) []string {
	prefix := ""
	if subdir != "" {
		prefix = subdir + "/"
	}
	var versions []string
	for _, t := range tags {
		v, ok := strings.CutPrefix(t, prefix)
		if ok && versionRe.MatchString(v) {
			versions = append(versions, v)
		}
	}
	return versions
}

type semver struct {
	nums [3]int
	pre  string
}

func parseVersion(v string) semver {
	var s semver
	core, pre, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	s.pre = pre
	for i, part := range strings.SplitN(core, ".", 3) {
		s.nums[i], _ = strconv.Atoi(part)
	}
	return s
}

func CompareVersions(a, b string) int {
	va, vb := parseVersion(a), parseVersion(b)
	for i := range va.nums {
		if va.nums[i] != vb.nums[i] {
			if va.nums[i] < vb.nums[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case va.pre == vb.pre:
		return 0
	case va.pre == "":
		return 1
	case vb.pre == "":
		return -1
	case va.pre < vb.pre:
		return -1
	}
	return 1
}

func latestVersion(versions []string) string {
	best := ""
	for _, v := range versions {
		if parseVersion(v).pre != "" {
			continue
		}
		if best == "" || CompareVersions(v, best) > 0 {
			best = v
		}
	}
	if best != "" {
		return best
	}
	for _, v := range versions {
		if best == "" || CompareVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

func locateModule(path string) (source, []string, error) {
	src, err := findRepo(path)
	if err != nil {
		return source{}, nil, err
	}
	tags, err := remoteTags(src.url)
	if err != nil {
		return source{}, nil, err
	}
	sub := src.subdir
	for {
		if versions := versionsIn(tags, sub); len(versions) > 0 {
			found := src
			found.subdir = sub
			return found, versions, nil
		}
		if sub == "" {
			break
		}
		if i := strings.LastIndex(sub, "/"); i >= 0 {
			sub = sub[:i]
		} else {
			sub = ""
		}
	}
	want := "v1.0.0"
	if src.subdir != "" {
		want = src.subdir + "/v1.0.0"
	}
	return source{}, nil, fmt.Errorf("%s has no version tags for %q — a release is a git tag such as %s", src.repo, path, want)
}

func modulePathOf(src source) string {
	if src.subdir == "" {
		return src.repo
	}
	return src.repo + "/" + src.subdir
}

func HashDir(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	summary := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(summary, "%s  %s\n", hex.EncodeToString(sum[:]), f)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(summary.Sum(nil)), nil
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(to, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o444)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}

func fetch(cache string, src source, version string) (string, error) {
	path := modulePathOf(src)
	dir := ModuleDir(cache, path, version)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".fetch-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	clone := filepath.Join(tmp, "repo")
	tag := tagFor(src.subdir, version)
	if _, err := git("-c", "advice.detachedHead=false", "clone", "--quiet", "--depth", "1", "--branch", tag, src.url, clone); err != nil {
		return "", fmt.Errorf("fetching %s@%s: %v", path, version, err)
	}
	root := filepath.Join(clone, filepath.FromSlash(src.subdir))
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s at tag %s has no folder %s", src.repo, tag, src.subdir)
	}
	if data, err := os.ReadFile(filepath.Join(root, ModFileName)); err == nil {
		own, err := ParseMod(filepath.Join(root, ModFileName), string(data))
		if err != nil {
			return "", err
		}
		if own.Path != path {
			return "", fmt.Errorf("%s@%s declares itself %s in its %s — the module path and the folder it lives in must agree", path, version, own.Path, ModFileName)
		}
	}
	staged := filepath.Join(tmp, "module")
	if err := copyTree(root, staged); err != nil {
		return "", err
	}
	if err := os.Rename(staged, dir); err != nil {
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

type Sums map[string]string

func sumKey(path, version string) string {
	return path + " " + version
}

func ReadSums(file string) (Sums, error) {
	sums := Sums{}
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return sums, nil
	}
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; scanner.Scan(); n++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "h1:") {
			return nil, fmt.Errorf("%s:%d: expected `<module> <version> h1:<hash>`", file, n)
		}
		sums[sumKey(fields[0], fields[1])] = fields[2]
	}
	return sums, nil
}

func (s Sums) Write(file string) error {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + " " + s[k] + "\n")
	}
	return os.WriteFile(file, []byte(b.String()), 0o644)
}

func (s Sums) check(path, version, dir string) error {
	got, err := HashDir(dir)
	if err != nil {
		return err
	}
	want, known := s[sumKey(path, version)]
	if !known {
		s[sumKey(path, version)] = got
		return nil
	}
	if want != got {
		return fmt.Errorf("%s@%s does not match %s: expected %s, have %s — the release changed after it was first downloaded, or the cache at %s was edited",
			path, version, SumFileName, want, got, dir)
	}
	return nil
}

func SetRequire(file, path, version string, indirect bool) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	entry := path + " " + version
	if indirect {
		entry += " // indirect"
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	block, blockEnd := "", -1
	for i, line := range lines {
		code := line
		if j := strings.Index(code, "//"); j >= 0 {
			code = code[:j]
		}
		fields := strings.Fields(code)
		if len(fields) == 0 {
			continue
		}
		if block == "" && len(fields) == 2 && fields[1] == "(" {
			block = fields[0]
			continue
		}
		if block != "" && len(fields) == 1 && fields[0] == ")" {
			if block == "require" {
				blockEnd = i
			}
			block = ""
			continue
		}
		switch {
		case block == "require" && len(fields) == 2 && fields[0] == path:
			lines[i] = "    " + entry
			return writeLines(file, lines)
		case block == "" && len(fields) == 3 && fields[0] == "require" && fields[1] == path:
			lines[i] = "require " + entry
			return writeLines(file, lines)
		}
	}
	if blockEnd >= 0 {
		lines = append(lines[:blockEnd], append([]string{"    " + entry}, lines[blockEnd:]...)...)
		return writeLines(file, lines)
	}
	var singles []int
	for i, line := range lines {
		if strings.HasPrefix(line, "require ") {
			singles = append(singles, i)
		}
	}
	if len(singles) == 0 {
		return writeLines(file, append(lines, "", "require "+entry))
	}
	folded := []string{"require ("}
	drop := map[int]bool{}
	for _, i := range singles {
		folded = append(folded, "    "+strings.TrimPrefix(lines[i], "require "))
		drop[i] = true
	}
	folded = append(folded, "    "+entry, ")")
	var out []string
	for i, line := range lines {
		if i == singles[0] {
			out = append(out, folded...)
		} else if !drop[i] {
			out = append(out, line)
		}
	}
	return writeLines(file, out)
}

func writeLines(file string, lines []string) error {
	return os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

type Fetched struct {
	Path     string
	Version  string
	Dir      string
	Indirect bool
	// NeedsDex is set when the module's dex.mod needs a newer compiler than
	// this one: the version it needs. The fetch still happens, because the
	// module may be the one the user is about to upgrade the compiler for, but
	// `dex get` says so now rather than at the next build.
	NeedsDex string
}

func Get(mod *Mod, arg string) ([]Fetched, error) {
	path, version, _ := strings.Cut(arg, "@")
	if !modPathRe.MatchString(path) || !IsModulePath(path) {
		return nil, fmt.Errorf("%q is not a module path — name it by its URL, as in github.com/acme/greet", path)
	}
	if version != "" && version != "latest" && !versionRe.MatchString(version) {
		return nil, fmt.Errorf("%q is not a version — write @v1.2.0, or @latest", version)
	}
	if within(path, mod.Path) {
		return nil, fmt.Errorf("%s is this module (%s) — its files are imported directly, not fetched", path, mod.Path)
	}
	cache, err := ModCache()
	if err != nil {
		return nil, err
	}
	sums, err := ReadSums(filepath.Join(mod.Dir, SumFileName))
	if err != nil {
		return nil, err
	}

	src, versions, err := locateModule(path)
	if err != nil {
		return nil, err
	}
	if version == "" || version == "latest" {
		version = latestVersion(versions)
	} else if !contains(versions, version) {
		return nil, fmt.Errorf("%s has no release %s — it has %s", modulePathOf(src), version, strings.Join(versions, ", "))
	}

	var fetched []Fetched
	queue := []Fetched{{Path: modulePathOf(src), Version: version}}
	sources := map[string]source{modulePathOf(src): src}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if have, ok := mod.Require[next.Path]; ok && next.Indirect && CompareVersions(have, next.Version) >= 0 {
			continue
		}
		s, ok := sources[next.Path]
		if !ok {
			if s, _, err = locateModule(next.Path); err != nil {
				return nil, err
			}
		}
		dir, err := fetch(cache, s, next.Version)
		if err != nil {
			return nil, err
		}
		if err := sums.check(next.Path, next.Version, dir); err != nil {
			return nil, err
		}
		next.Dir = dir
		mod.Require[next.Path] = next.Version
		if err := SetRequire(mod.File, next.Path, next.Version, next.Indirect); err != nil {
			return nil, err
		}
		fetched = append(fetched, next)

		if data, err := os.ReadFile(filepath.Join(dir, ModFileName)); err == nil {
			own, err := ParseMod(filepath.Join(dir, ModFileName), string(data))
			if err != nil {
				return nil, err
			}
			if !compilerSatisfies(compilerVersion, own.Dex) {
				fetched[len(fetched)-1].NeedsDex = own.Dex
			}
			for dep, v := range own.Require {
				if dep == mod.Path {
					continue
				}
				queue = append(queue, Fetched{Path: dep, Version: v, Indirect: true})
			}
		}
	}
	if err := sums.Write(filepath.Join(mod.Dir, SumFileName)); err != nil {
		return nil, err
	}
	return fetched, nil
}

func Download(mod *Mod) ([]Fetched, error) {
	cache, err := ModCache()
	if err != nil {
		return nil, err
	}
	sums, err := ReadSums(filepath.Join(mod.Dir, SumFileName))
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(mod.Require))
	for p := range mod.Require {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var fetched []Fetched
	for _, p := range paths {
		if _, replaced := mod.Replace[p]; replaced {
			continue
		}
		v := mod.Require[p]
		dir := ModuleDir(cache, p, v)
		if _, err := os.Stat(dir); err != nil {
			src, _, err := locateModule(p)
			if err != nil {
				return nil, err
			}
			if dir, err = fetch(cache, src, v); err != nil {
				return nil, err
			}
		}
		if err := sums.check(p, v, dir); err != nil {
			return nil, err
		}
		fetched = append(fetched, Fetched{Path: p, Version: v, Dir: dir})
	}
	if err := sums.Write(filepath.Join(mod.Dir, SumFileName)); err != nil {
		return nil, err
	}
	return fetched, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
