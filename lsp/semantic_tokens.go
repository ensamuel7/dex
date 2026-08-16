package lsp

import (
	"encoding/json"
	"sort"
	"unicode/utf8"

	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// LSP semantic token types — indices into this array are used in the protocol.
var semanticTokenTypes = []string{
	"namespace",  // 0
	"type",       // 1
	"enum",       // 2
	"parameter",  // 3
	"variable",   // 4
	"property",   // 5
	"enumMember", // 6
	"function",   // 7
}

const (
	semTypeNamespace  = 0
	semTypeType       = 1
	semTypeEnum       = 2
	semTypeParameter  = 3
	semTypeVariable   = 4
	semTypeProperty   = 5
	semTypeEnumMember = 6
	semTypeFunction   = 7
)

// LSP semantic token modifiers — bit positions.
var semanticTokenModifiers = []string{
	"declaration", // bit 0
	"readonly",    // bit 1
}

const (
	semModDeclaration = 1 << 0
	semModReadonly    = 1 << 1
)

// rawToken is an intermediate representation before delta-encoding.
type rawToken struct {
	line      int // 0-based
	col       int // 0-based
	length    int
	tokenType int
	modifiers int
}

func (s *Server) handleSemanticTokens(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	json.Unmarshal(msg.Params, &params)

	text, ok := s.documents[params.TextDocument.URI]
	if !ok {
		s.sendResponse(msg.ID, map[string]interface{}{"data": []int{}})
		return
	}

	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		s.sendResponse(msg.ID, map[string]interface{}{"data": []int{}})
		return
	}

	data := scanSemanticTokens(tokens)
	s.sendResponse(msg.ID, map[string]interface{}{"data": data})
}

// scanSemanticTokens walks the token stream and produces delta-encoded semantic tokens.
func scanSemanticTokens(tokens []token.Token) []int {
	// Collect known struct and enum names from the token stream.
	structNames := map[string]bool{}
	enumNames := map[string]bool{}

	// Add stdlib module-provided struct types.
	for _, mod := range stdlib.AllModules() {
		for _, td := range mod.Types {
			structNames[td.Name] = true
		}
	}
	structNames["Exception"] = true

	// Scan for user-defined struct/enum names.
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenStruct && tokens[i+1].Kind == token.TokenIdent {
			structNames[tokens[i+1].Value] = true
		}
		if tokens[i].Kind == token.TokenEnum && tokens[i+1].Kind == token.TokenIdent {
			enumNames[tokens[i+1].Value] = true
		}
	}

	// Collect imported module names (import path is the module name in DexLang).
	importedModules := map[string]bool{}
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			path := tokens[i+1].Value
			importedModules[path] = true
			// Check for alias: import "path" as "alias"
			if i+3 < len(tokens) && tokens[i+2].Kind == token.TokenAs && tokens[i+3].Kind == token.TokenString {
				importedModules[tokens[i+3].Value] = true
			}
		}
	}

	var raw []rawToken

	// Track struct literal context with a stack of brace depths.
	braceDepth := 0
	var structBraceStack []int // brace depths at which struct literals start

	// Track function parameter context.
	inFnParamList := false
	fnParamParenDepth := 0
	parenDepth := 0

	emit := func(tok token.Token, tokenType int, modifiers int) {
		length := utf8.RuneCountInString(tok.Value)
		if length == 0 {
			return
		}
		raw = append(raw, rawToken{
			line:      tok.Line - 1, // tokens are 1-based, LSP is 0-based
			col:       tok.Col - 1,
			length:    length,
			tokenType: tokenType,
			modifiers: modifiers,
		})
	}

	peek := func(i int) *token.Token {
		if i+1 < len(tokens) {
			return &tokens[i+1]
		}
		return nil
	}

	peek2 := func(i int) *token.Token {
		if i+2 < len(tokens) {
			return &tokens[i+2]
		}
		return nil
	}

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		// Track brace depth.
		if tok.Kind == token.TokenLBrace {
			braceDepth++
		}
		if tok.Kind == token.TokenRBrace {
			braceDepth--
			// Pop struct context if we're closing a struct literal.
			if len(structBraceStack) > 0 && structBraceStack[len(structBraceStack)-1] == braceDepth {
				structBraceStack = structBraceStack[:len(structBraceStack)-1]
			}
		}

		// Track paren depth.
		if tok.Kind == token.TokenLParen {
			parenDepth++
		}
		if tok.Kind == token.TokenRParen {
			parenDepth--
			if inFnParamList && parenDepth < fnParamParenDepth {
				inFnParamList = false
			}
		}

		switch {
		// "fn ident" or "function ident" → function declaration
		case (tok.Kind == token.TokenFn || tok.Kind == token.TokenFunction):
			if next := peek(i); next != nil && next.Kind == token.TokenIdent {
				emit(*next, semTypeFunction, semModDeclaration)
				// Enter param list tracking.
				if p2 := peek2(i); p2 != nil && p2.Kind == token.TokenLParen {
					inFnParamList = true
					fnParamParenDepth = parenDepth + 1 // will be incremented when we hit the LParen
				}
				i++ // skip the ident we just emitted
			}

		// "struct ident" → type declaration
		case tok.Kind == token.TokenStruct:
			if next := peek(i); next != nil && next.Kind == token.TokenIdent {
				emit(*next, semTypeType, semModDeclaration)
				i++
			}

		// "enum ident" → type/enum declaration
		case tok.Kind == token.TokenEnum:
			if next := peek(i); next != nil && next.Kind == token.TokenIdent {
				emit(*next, semTypeEnum, semModDeclaration)
				i++
			}

		// "let ident" → variable declaration
		case tok.Kind == token.TokenLet:
			if next := peek(i); next != nil && next.Kind == token.TokenIdent {
				emit(*next, semTypeVariable, semModDeclaration)
				i++
			}

		// "const ident" → variable declaration (readonly)
		case tok.Kind == token.TokenConst:
			if next := peek(i); next != nil && next.Kind == token.TokenIdent {
				emit(*next, semTypeVariable, semModDeclaration|semModReadonly)
				i++
			}

		// Identifier patterns
		case tok.Kind == token.TokenIdent:
			next := peek(i)

			// Struct literal start: StructName { ... }
			// ident followed by "{" where ident is a known struct name
			if next != nil && next.Kind == token.TokenLBrace && structNames[tok.Value] {
				emit(tok, semTypeType, 0)
				structBraceStack = append(structBraceStack, braceDepth)
				continue
			}

			// EnumName.Variant pattern
			if next != nil && next.Kind == token.TokenDot && enumNames[tok.Value] {
				p2 := peek2(i)
				if p2 != nil && p2.Kind == token.TokenIdent {
					emit(tok, semTypeEnum, 0)
					emit(*p2, semTypeEnumMember, 0)
					i += 2 // skip dot and variant
					continue
				}
			}

			// Module.func() pattern: ident "." ident "("
			if next != nil && next.Kind == token.TokenDot && importedModules[tok.Value] {
				p2 := peek2(i)
				if p2 != nil && p2.Kind == token.TokenIdent {
					emit(tok, semTypeNamespace, 0)
					// Check if it's a function call (followed by "(")
					if i+3 < len(tokens) && tokens[i+3].Kind == token.TokenLParen {
						emit(*p2, semTypeFunction, 0)
					}
					i += 2 // skip dot and func name
					continue
				}
			}

			// Inside struct literal: ident followed by ":" → property
			if len(structBraceStack) > 0 && next != nil && next.Kind == token.TokenColon {
				emit(tok, semTypeProperty, 0)
				continue
			}

			// Function parameters: inside fn param list, ident before ":"
			if inFnParamList && parenDepth >= fnParamParenDepth {
				if next != nil && next.Kind == token.TokenColon {
					emit(tok, semTypeParameter, semModDeclaration)
					continue
				}
			}

			// Type annotation: ":" followed by a known struct/enum name
			// Check if this ident IS a type name appearing after a ":"
			if i >= 1 && tokens[i-1].Kind == token.TokenColon {
				if structNames[tok.Value] {
					emit(tok, semTypeType, 0)
					continue
				}
				if enumNames[tok.Value] {
					emit(tok, semTypeEnum, 0)
					continue
				}
			}
		}
	}

	// Sort by position (line, then column).
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].line != raw[j].line {
			return raw[i].line < raw[j].line
		}
		return raw[i].col < raw[j].col
	})

	// Delta-encode into the LSP format.
	data := make([]int, 0, len(raw)*5)
	prevLine := 0
	prevCol := 0
	for _, r := range raw {
		deltaLine := r.line - prevLine
		deltaCol := r.col
		if deltaLine == 0 {
			deltaCol = r.col - prevCol
		}
		data = append(data, deltaLine, deltaCol, r.length, r.tokenType, r.modifiers)
		prevLine = r.line
		prevCol = r.col
	}

	return data
}
