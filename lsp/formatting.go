package lsp

import (
	"encoding/json"
	"strings"

	"github.com/ensamuel7/dex/format"
)

// handleFormatting answers textDocument/formatting with a single edit replacing
// the whole file, which is how a whole-file formatter is normally reported: the
// client diffs it to decide what actually moved.
//
// A file that does not lex, or that the formatter cannot account for, produces
// no edits at all rather than an error. Format-on-save runs while code is being
// typed and is therefore nearly always mid-edit; refusing to format an
// incomplete file is right, but telling the editor about it on every keystroke
// is not.
func (s *Server) handleFormatting(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	json.Unmarshal(msg.Params, &params)

	text, ok := s.documents[params.TextDocument.URI]
	if !ok {
		s.sendResponse(msg.ID, []TextEdit{})
		return
	}

	out, err := format.Source(text, uriToPath(params.TextDocument.URI))
	if err != nil || out == text {
		s.sendResponse(msg.ID, []TextEdit{})
		return
	}

	s.sendResponse(msg.ID, []TextEdit{{
		Range:   wholeDocument(text),
		NewText: out,
	}})
}

// wholeDocument is the range covering every character of text. The end position
// is one past the last line's final character, counted in UTF-16 code units
// because that is what an LSP position is measured in.
func wholeDocument(text string) Range {
	lines := strings.Split(text, "\n")
	last := len(lines) - 1
	return Range{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: last, Character: utf16Len(lines[last])},
	}
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
			continue
		}
		n++
	}
	return n
}
