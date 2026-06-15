package lexer

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/token"
)

func TestSingleCharTokens(t *testing.T) {
	tests := []struct {
		input string
		kind  token.TokenKind
		value string
	}{
		{"+", token.TokenPlus, "+"},
		{"-", token.TokenMinus, "-"},
		{"*", token.TokenStar, "*"},
		{"/", token.TokenSlash, "/"},
		{"=", token.TokenAssign, "="},
		{"!", token.TokenBang, "!"},
		{"<", token.TokenLt, "<"},
		{">", token.TokenGt, ">"},
		{"(", token.TokenLParen, "("},
		{")", token.TokenRParen, ")"},
		{"{", token.TokenLBrace, "{"},
		{"}", token.TokenRBrace, "}"},
		{":", token.TokenColon, ":"},
		{",", token.TokenComma, ","},
		{".", token.TokenDot, "."},
		{"[", token.TokenLBracket, "["},
		{"]", token.TokenRBracket, "]"},
		{"%", token.TokenPercent, "%"},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if len(tokens) != 2 { // token + EOF
			t.Fatalf("Tokenize(%q) returned %d tokens, want 2", tt.input, len(tokens))
		}
		if tokens[0].Kind != tt.kind {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want %d", tt.input, tokens[0].Kind, tt.kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestThreeCharTokens(t *testing.T) {
	tests := []struct {
		input string
		kind  token.TokenKind
		value string
	}{
		{"===", token.TokenStrictEq, "==="},
		{"!==", token.TokenStrictNeq, "!=="},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if len(tokens) != 2 {
			t.Fatalf("Tokenize(%q) returned %d tokens, want 2", tt.input, len(tokens))
		}
		if tokens[0].Kind != tt.kind {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want %d", tt.input, tokens[0].Kind, tt.kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestTwoCharTokens(t *testing.T) {
	tests := []struct {
		input string
		kind  token.TokenKind
		value string
	}{
		{"==", token.TokenEq, "=="},
		{"!=", token.TokenNeq, "!="},
		{"<=", token.TokenLte, "<="},
		{">=", token.TokenGte, ">="},
		{"&&", token.TokenAnd, "&&"},
		{"||", token.TokenOr, "||"},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if len(tokens) != 2 {
			t.Fatalf("Tokenize(%q) returned %d tokens, want 2", tt.input, len(tokens))
		}
		if tokens[0].Kind != tt.kind {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want %d", tt.input, tokens[0].Kind, tt.kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestKeywords(t *testing.T) {
	tests := []struct {
		keyword string
		kind    token.TokenKind
	}{
		{"fn", token.TokenFn},
		{"function", token.TokenFunction},
		{"let", token.TokenLet},
		{"return", token.TokenReturn},
		{"if", token.TokenIf},
		{"else", token.TokenElse},
		{"while", token.TokenWhile},
		{"true", token.TokenTrue},
		{"false", token.TokenFalse},
		{"import", token.TokenImport},
		{"int", token.TokenIntKw},
		{"bool", token.TokenBool},
		{"string", token.TokenStringKw},
		{"long", token.TokenLong},
		{"double", token.TokenDouble},
	}
	for _, tt := range tests {
		tokens, err := New(tt.keyword).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.keyword, err)
		}
		if tokens[0].Kind != tt.kind {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want %d", tt.keyword, tokens[0].Kind, tt.kind)
		}
		if tokens[0].Value != tt.keyword {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.keyword, tokens[0].Value, tt.keyword)
		}
	}
}

func TestIntegerLiterals(t *testing.T) {
	tests := []string{"0", "1", "42", "12345"}
	for _, input := range tests {
		tokens, err := New(input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", input, err)
		}
		if tokens[0].Kind != token.TokenInt {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want TokenInt", input, tokens[0].Kind)
		}
		if tokens[0].Value != input {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", input, tokens[0].Value, input)
		}
	}
}

func TestFloatLiterals(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{"3.14", "3.14"},
		{"0.5", "0.5"},
		{"100.001", "100.001"},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if tokens[0].Kind != token.TokenFloat {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want TokenFloat", tt.input, tokens[0].Kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestStringLiterals(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{`"hello"`, "hello"},
		{`""`, ""},
		{`"hello world"`, "hello world"},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if tokens[0].Kind != token.TokenString {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want TokenString", tt.input, tokens[0].Kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestStringEscapeSequences(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{`"hello\nworld"`, "hello\nworld"},
		{`"tab\there"`, "tab\there"},
		{`"back\\slash"`, "back\\slash"},
		{`"say \"hi\""`, "say \"hi\""},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if tokens[0].Kind != token.TokenString {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want TokenString", tt.input, tokens[0].Kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	tests := []string{"x", "foo", "myVar", "hello_world", "_private", "x1"}
	for _, input := range tests {
		tokens, err := New(input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", input, err)
		}
		if tokens[0].Kind != token.TokenIdent {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want TokenIdent", input, tokens[0].Kind)
		}
		if tokens[0].Value != input {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", input, tokens[0].Value, input)
		}
	}
}

func TestLineComments(t *testing.T) {
	input := "42 // this is a comment\n7"
	tokens, err := New(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	// Should produce: 42, 7, EOF
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d", len(tokens))
	}
	if tokens[0].Kind != token.TokenInt || tokens[0].Value != "42" {
		t.Errorf("token[0] = %v, want TokenInt(42)", tokens[0])
	}
	if tokens[1].Kind != token.TokenInt || tokens[1].Value != "7" {
		t.Errorf("token[1] = %v, want TokenInt(7)", tokens[1])
	}
}

func TestWhitespaceSkipped(t *testing.T) {
	input := "  42  \t  7  \n  3  "
	tokens, err := New(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if len(tokens) != 4 { // 42, 7, 3, EOF
		t.Fatalf("expected 4 tokens, got %d", len(tokens))
	}
}

func TestPositionTracking(t *testing.T) {
	input := "x\ny"
	tokens, err := New(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if tokens[0].Line != 1 || tokens[0].Col != 1 {
		t.Errorf("token 'x' position = %d:%d, want 1:1", tokens[0].Line, tokens[0].Col)
	}
	if tokens[1].Line != 2 || tokens[1].Col != 1 {
		t.Errorf("token 'y' position = %d:%d, want 2:1", tokens[1].Line, tokens[1].Col)
	}
}

func TestPositionTrackingColumns(t *testing.T) {
	input := "x + y"
	tokens, err := New(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	// x at 1:1, + at 1:3, y at 1:5
	if tokens[0].Line != 1 || tokens[0].Col != 1 {
		t.Errorf("'x' position = %d:%d, want 1:1", tokens[0].Line, tokens[0].Col)
	}
	if tokens[1].Line != 1 || tokens[1].Col != 3 {
		t.Errorf("'+' position = %d:%d, want 1:3", tokens[1].Line, tokens[1].Col)
	}
	if tokens[2].Line != 1 || tokens[2].Col != 5 {
		t.Errorf("'y' position = %d:%d, want 1:5", tokens[2].Line, tokens[2].Col)
	}
}

func TestUnterminatedString(t *testing.T) {
	input := `"hello`
	_, err := New(input).Tokenize()
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
	if !strings.Contains(err.Error(), "unterminated string") {
		t.Errorf("error = %q, want to contain 'unterminated string'", err.Error())
	}
}

func TestUnknownEscapeSequence(t *testing.T) {
	input := `"hello\x"`
	_, err := New(input).Tokenize()
	if err == nil {
		t.Fatal("expected error for unknown escape sequence")
	}
	if !strings.Contains(err.Error(), "unknown escape sequence") {
		t.Errorf("error = %q, want to contain 'unknown escape sequence'", err.Error())
	}
}

func TestUnexpectedCharacter(t *testing.T) {
	input := "@"
	_, err := New(input).Tokenize()
	if err == nil {
		t.Fatal("expected error for unexpected character")
	}
	if !strings.Contains(err.Error(), "unexpected character") {
		t.Errorf("error = %q, want to contain 'unexpected character'", err.Error())
	}
}

func TestMultipleTokens(t *testing.T) {
	input := "let x: int = 42"
	tokens, err := New(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	expected := []struct {
		kind  token.TokenKind
		value string
	}{
		{token.TokenLet, "let"},
		{token.TokenIdent, "x"},
		{token.TokenColon, ":"},
		{token.TokenIntKw, "int"},
		{token.TokenAssign, "="},
		{token.TokenInt, "42"},
		{token.TokenEOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}
	for i, exp := range expected {
		if tokens[i].Kind != exp.kind || tokens[i].Value != exp.value {
			t.Errorf("token[%d] = {Kind:%d, Value:%q}, want {Kind:%d, Value:%q}",
				i, tokens[i].Kind, tokens[i].Value, exp.kind, exp.value)
		}
	}
}

func TestEOFToken(t *testing.T) {
	tokens, err := New("").Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token (EOF), got %d", len(tokens))
	}
	if tokens[0].Kind != token.TokenEOF {
		t.Errorf("expected EOF token, got %d", tokens[0].Kind)
	}
}

func TestNewTwoCharTokens(t *testing.T) {
	tests := []struct {
		input string
		kind  token.TokenKind
		value string
	}{
		{"++", token.TokenPlusPlus, "++"},
		{"--", token.TokenMinusMinus, "--"},
		{"+=", token.TokenPlusAssign, "+="},
		{"-=", token.TokenMinusAssign, "-="},
	}
	for _, tt := range tests {
		tokens, err := New(tt.input).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.input, err)
		}
		if len(tokens) != 2 {
			t.Fatalf("Tokenize(%q) returned %d tokens, want 2", tt.input, len(tokens))
		}
		if tokens[0].Kind != tt.kind {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want %d", tt.input, tokens[0].Kind, tt.kind)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("Tokenize(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestSemicolonToken(t *testing.T) {
	tokens, err := New(";").Tokenize()
	if err != nil {
		t.Fatalf("Tokenize(;) error: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].Kind != token.TokenSemicolon {
		t.Errorf("expected TokenSemicolon, got %d", tokens[0].Kind)
	}
}

func TestNewKeywords(t *testing.T) {
	tests := []struct {
		keyword string
		kind    token.TokenKind
	}{
		{"break", token.TokenBreak},
		{"continue", token.TokenContinue},
		{"for", token.TokenFor},
		{"foreach", token.TokenForeach},
		{"as", token.TokenAs},
	}
	for _, tt := range tests {
		tokens, err := New(tt.keyword).Tokenize()
		if err != nil {
			t.Fatalf("Tokenize(%q) error: %v", tt.keyword, err)
		}
		if tokens[0].Kind != tt.kind {
			t.Errorf("Tokenize(%q)[0].Kind = %d, want %d", tt.keyword, tokens[0].Kind, tt.kind)
		}
	}
}

func TestForLoopTokens(t *testing.T) {
	input := "for(let i = 0; i < 10; i++)"
	tokens, err := New(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	// for ( let i = 0 ; i < 10 ; i ++ ) EOF
	expected := []token.TokenKind{
		token.TokenFor, token.TokenLParen, token.TokenLet, token.TokenIdent,
		token.TokenAssign, token.TokenInt, token.TokenSemicolon, token.TokenIdent,
		token.TokenLt, token.TokenInt, token.TokenSemicolon, token.TokenIdent,
		token.TokenPlusPlus, token.TokenRParen, token.TokenEOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}
	for i, exp := range expected {
		if tokens[i].Kind != exp {
			t.Errorf("token[%d].Kind = %d, want %d (value=%q)", i, tokens[i].Kind, exp, tokens[i].Value)
		}
	}
}
