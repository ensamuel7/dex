package lsp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
)

// --- LSP types ---

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
}

const codeActionQuickFix = "quickfix"

// The two checker errors an import fixes: a qualified call into a module the
// file never imported, and a bare call to a name some module exports.
var (
	missingModuleRe = regexp.MustCompile(`^module '([^']+)' is not imported$`)
	missingFuncRe   = regexp.MustCompile(`^undefined function '([^']+)'$`)
)

func (s *Server) handleCodeAction(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range   Range `json:"range"`
		Context struct {
			Diagnostics []Diagnostic `json:"diagnostics"`
		} `json:"context"`
	}
	json.Unmarshal(msg.Params, &params)

	text, ok := s.documents[params.TextDocument.URI]
	if !ok {
		s.sendResponse(msg.ID, []CodeAction{})
		return
	}

	// The client normally hands us the diagnostics under the cursor; fall back
	// to the ones we last published for the file when it does not.
	diags := params.Context.Diagnostics
	if len(diags) == 0 {
		diags = s.lastDiags[params.TextDocument.URI]
	}

	s.sendResponse(msg.ID, s.importCodeActions(params.TextDocument.URI, text, params.Range, diags))
}

// importCodeActions offers an import for every unresolved name in range.
func (s *Server) importCodeActions(uri, text string, rng Range, diags []Diagnostic) []CodeAction {
	filePath := uriToPath(uri)
	sourceDir := filepath.Dir(filePath)

	actions := []CodeAction{}
	seen := map[string]bool{}
	add := func(a CodeAction) {
		if seen[a.Title] {
			return
		}
		seen[a.Title] = true
		actions = append(actions, a)
	}

	for _, d := range diags {
		if !rangesOverlap(d.Range, rng) {
			continue
		}

		// `userService.init(1)` with no import: insert the import line.
		if m := missingModuleRe.FindStringSubmatch(d.Message); m != nil {
			for _, path := range importPathsForModule(m[1], sourceDir, filePath) {
				add(CodeAction{
					Title:       fmt.Sprintf("Import %q", path),
					Kind:        codeActionQuickFix,
					Diagnostics: []Diagnostic{d},
					IsPreferred: true,
					Edit:        &WorkspaceEdit{Changes: map[string][]TextEdit{uri: buildImportEdit(path, text)}},
				})
			}
			continue
		}

		// `println("hi")`: the name only resolves qualified, so the fix is the
		// import plus rewriting the call site to go through the module.
		if m := missingFuncRe.FindStringSubmatch(d.Message); m != nil {
			fnName := m[1]
			for _, cand := range modulesExporting(fnName, sourceDir, filePath) {
				qualify := TextEdit{Range: d.Range, NewText: cand.name + "." + fnName}
				if isModuleImported(cand.path, text) {
					add(CodeAction{
						Title:       fmt.Sprintf("Use %s.%s", cand.name, fnName),
						Kind:        codeActionQuickFix,
						Diagnostics: []Diagnostic{d},
						IsPreferred: true,
						Edit:        &WorkspaceEdit{Changes: map[string][]TextEdit{uri: {qualify}}},
					})
					continue
				}
				edits := append(buildImportEdit(cand.path, text), qualify)
				add(CodeAction{
					Title:       fmt.Sprintf("Import %q and use %s.%s", cand.path, cand.name, fnName),
					Kind:        codeActionQuickFix,
					Diagnostics: []Diagnostic{d},
					Edit:        &WorkspaceEdit{Changes: map[string][]TextEdit{uri: edits}},
				})
			}
		}
	}

	return actions
}

// importPathsForModule returns the import path that gives a file access to the
// named module — the stdlib name, or the user .dx file that defines it.
func importPathsForModule(moduleName, sourceDir, selfPath string) []string {
	var paths []string
	if stdlibModuleExists(moduleName) {
		paths = append(paths, moduleName)
	}
	if impPath, _, ok := userModuleNamed(moduleName, sourceDir, selfPath); ok {
		paths = append(paths, impPath)
	}
	return paths
}

// rangesOverlap reports whether two ranges touch, which is how a cursor or a
// selection is matched against a published diagnostic.
func rangesOverlap(a, b Range) bool {
	return !positionBefore(a.End, b.Start) && !positionBefore(b.End, a.Start)
}

func positionBefore(p, q Position) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Character < q.Character
}
