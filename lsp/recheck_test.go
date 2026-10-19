package lsp

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func lastDiagnosticsFor(out string, uri string) ([]Diagnostic, bool) {
	frame := regexp.MustCompile(`(?s)Content-Length: \d+\r\n\r\n`)
	var last []Diagnostic
	found := false
	for _, part := range frame.Split(out, -1) {
		var msg struct {
			Method string `json:"method"`
			Params struct {
				URI         string       `json:"uri"`
				Diagnostics []Diagnostic `json:"diagnostics"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(part), &msg) != nil || msg.Method != "textDocument/publishDiagnostics" {
			continue
		}
		if msg.Params.URI == uri {
			last = msg.Params.Diagnostics
			found = true
		}
	}
	return last, found
}

func TestSavingAModuleRechecksItsImporters(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "store.dx")
	mainPath := filepath.Join(dir, "main.dx")
	mainSrc := "import \"store\"\n\nfn main(): void {\n    assert(store.count(1) == 1)\n}\n"
	if err := os.WriteFile(modPath, []byte("fn count(a: int): int {\n    return a\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	mainURI := pathToURI(mainPath)
	modURI := pathToURI(modPath)

	buf := &bytes.Buffer{}
	s := &Server{
		documents:    map[string]string{},
		lastDiags:    map[string][]Diagnostic{},
		writer:       buf,
		logger:       log.New(io.Discard, "", 0),
		importedURIs: map[string][]string{},
	}
	open := func(uri, text string) {
		params, _ := json.Marshal(map[string]interface{}{"textDocument": map[string]string{"uri": uri, "text": text}})
		s.handleMessage(&jsonrpcMessage{Method: "textDocument/didOpen", Params: params})
	}
	open(mainURI, mainSrc)
	if diags, _ := lastDiagnosticsFor(buf.String(), mainURI); len(diags) != 0 {
		t.Fatalf("unexpected diagnostics before the change: %v", diags)
	}

	renamed := "fn total(a: int): int {\n    return a\n}\n"
	if err := os.WriteFile(modPath, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	open(modURI, renamed)
	buf.Reset()
	params, _ := json.Marshal(map[string]interface{}{"textDocument": map[string]string{"uri": modURI}})
	s.handleMessage(&jsonrpcMessage{Method: "textDocument/didSave", Params: params})

	diags, found := lastDiagnosticsFor(buf.String(), mainURI)
	if !found || len(diags) == 0 {
		t.Fatalf("saving the module did not recheck its importer; got %v", diags)
	}

	if err := os.WriteFile(modPath, []byte("fn count(a: int): int {\n    return a\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	s.handleMessage(&jsonrpcMessage{Method: "workspace/didChangeWatchedFiles", Params: json.RawMessage(`{"changes":[]}`)})
	diags, found = lastDiagnosticsFor(buf.String(), mainURI)
	if !found || len(diags) != 0 {
		t.Fatalf("a change on disk did not clear the importer's diagnostics; got %v", diags)
	}
}

func TestClientResponseIsNotAnswered(t *testing.T) {
	buf := &bytes.Buffer{}
	s := &Server{writer: buf, logger: log.New(io.Discard, "", 0)}
	id := json.RawMessage(`"dex-watch"`)
	s.handleMessage(&jsonrpcMessage{ID: &id})
	if buf.Len() != 0 {
		t.Fatalf("replied to a response: %s", buf.String())
	}
}
