// Package profview turns the JSON a profile build writes at exit into one
// self-contained HTML page: a memory-over-time chart, a sortable table of the
// .dx lines that allocate and hold memory, and the source with each line's
// share beside it. Nothing is loaded from the network, so the page works
// opened straight off the disk and can be passed around as a single file.
package profview

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed template.html
var pageTemplate string

// Profile is the runtime's JSON, as far as the viewer needs to read it in Go.
// The page itself reads every field, so the raw bytes are passed through
// rather than re-encoded from this.
type Profile struct {
	Version int    `json:"version"`
	Sites   []Site `json:"sites"`
}

// Site is one .dx statement's tally.
type Site struct {
	Site string `json:"site"`
}

// SplitSite separates "path/file.dx:12" into its file and line. A marker that
// is not of that form — "<program start>", "<other sites>" — has no file.
func SplitSite(site string) (string, int) {
	i := strings.LastIndexByte(site, ':')
	if i <= 0 {
		return "", 0
	}
	n, err := strconv.Atoi(site[i+1:])
	if err != nil || n <= 0 {
		return "", 0
	}
	return site[:i], n
}

// Sources reads every source file a profile names, keyed as the profile names
// it. A path is read as written, and then relative to baseDir, since the
// markers carry the path the compiler was given and the viewer may be built
// from somewhere else. A file that cannot be read is left out: the table still
// works, the source pane says it is missing.
func Sources(p *Profile, baseDir string) map[string]string {
	out := map[string]string{}
	for _, s := range p.Sites {
		file, _ := SplitSite(s.Site)
		if file == "" {
			continue
		}
		if _, done := out[file]; done {
			continue
		}
		candidates := []string{file}
		if !filepath.IsAbs(file) && baseDir != "" {
			candidates = append(candidates, filepath.Join(baseDir, file))
		}
		for _, c := range candidates {
			if b, err := os.ReadFile(c); err == nil {
				out[file] = string(b)
				break
			}
		}
	}
	return out
}

type pageData struct {
	Title string
	Data  template.JS
}

// Render builds the page for a profile. raw is the runtime's JSON exactly as
// written; title names the program in the page heading; baseDir is where the
// program's source files are looked for when the paths in the profile are
// relative to somewhere other than the current directory.
func Render(raw []byte, title, baseDir string) ([]byte, error) {
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("not a dex profile: %v", err)
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("unsupported profile version %d", p.Version)
	}
	sources := Sources(&p, baseDir)
	files := make([]string, 0, len(sources))
	for f := range sources {
		files = append(files, f)
	}
	sort.Strings(files)

	// json.Marshal escapes <, > and & inside strings, so neither a source
	// file nor a path can close the script element the data sits in.
	payload, err := json.Marshal(struct {
		Title   string            `json:"title"`
		Profile json.RawMessage   `json:"profile"`
		Sources map[string]string `json:"sources"`
		Files   []string          `json:"files"`
	}{title, json.RawMessage(raw), sources, files})
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("profile").Parse(pageTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, pageData{Title: title, Data: template.JS(payload)}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
