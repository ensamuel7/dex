package token

import "fmt"

type TokenKind int

const (
	TokenFn TokenKind = iota
	TokenFunction
	TokenLet
	TokenConst
	TokenReturn
	TokenIf
	TokenElse
	TokenWhile
	TokenTrue
	TokenFalse
	TokenImport
	TokenStruct
	TokenBreak
	TokenContinue
	TokenFor
	TokenForeach
	TokenAs
	TokenIntKw
	TokenBool
	TokenStringKw
	TokenLong
	TokenDouble
	TokenVoid
	TokenIdent
	TokenInt
	TokenFloat
	TokenString
	TokenPercent
	TokenPlus
	TokenMinus
	TokenStar
	TokenSlash
	TokenEq
	TokenNeq
	TokenStrictEq
	TokenStrictNeq
	TokenLt
	TokenGt
	TokenLte
	TokenGte
	TokenAnd
	TokenOr
	TokenAssign
	TokenBang
	TokenLParen
	TokenRParen
	TokenLBrace
	TokenRBrace
	TokenColon
	TokenComma
	TokenDot
	TokenLBracket
	TokenRBracket
	TokenSemicolon
	TokenPlusPlus
	TokenMinusMinus
	TokenPlusAssign
	TokenMinusAssign
	TokenPublic
	TokenPrivate
	TokenSpawn
	TokenChan
	TokenCharKw
	TokenChar
	TokenAnnotation
	TokenWeak
	TokenQuestion
	TokenNull
	TokenAmpersand
	TokenTry
	TokenCatch
	TokenFinally
	TokenThrow
	TokenSwitch
	TokenCase
	TokenDefault
	TokenEOF
)

type Token struct {
	Kind  TokenKind
	Value string
	Line  int
	Col   int
}

func (k TokenKind) String() string {
	switch k {
	case TokenFn:
		return "fn"
	case TokenFunction:
		return "function"
	case TokenLet:
		return "let"
	case TokenConst:
		return "const"
	case TokenReturn:
		return "return"
	case TokenIf:
		return "if"
	case TokenElse:
		return "else"
	case TokenWhile:
		return "while"
	case TokenTrue:
		return "true"
	case TokenFalse:
		return "false"
	case TokenImport:
		return "import"
	case TokenStruct:
		return "struct"
	case TokenBreak:
		return "break"
	case TokenContinue:
		return "continue"
	case TokenFor:
		return "for"
	case TokenForeach:
		return "foreach"
	case TokenAs:
		return "as"
	case TokenIntKw:
		return "int"
	case TokenBool:
		return "bool"
	case TokenStringKw:
		return "string"
	case TokenLong:
		return "long"
	case TokenDouble:
		return "double"
	case TokenVoid:
		return "void"
	case TokenIdent:
		return "identifier"
	case TokenInt:
		return "integer literal"
	case TokenFloat:
		return "float literal"
	case TokenString:
		return "string literal"
	case TokenPercent:
		return "%"
	case TokenPlus:
		return "+"
	case TokenMinus:
		return "-"
	case TokenStar:
		return "*"
	case TokenSlash:
		return "/"
	case TokenEq:
		return "=="
	case TokenNeq:
		return "!="
	case TokenStrictEq:
		return "==="
	case TokenStrictNeq:
		return "!=="
	case TokenLt:
		return "<"
	case TokenGt:
		return ">"
	case TokenLte:
		return "<="
	case TokenGte:
		return ">="
	case TokenAnd:
		return "&&"
	case TokenOr:
		return "||"
	case TokenAssign:
		return "="
	case TokenBang:
		return "!"
	case TokenLParen:
		return "("
	case TokenRParen:
		return ")"
	case TokenLBrace:
		return "{"
	case TokenRBrace:
		return "}"
	case TokenColon:
		return ":"
	case TokenComma:
		return ","
	case TokenDot:
		return "."
	case TokenLBracket:
		return "["
	case TokenRBracket:
		return "]"
	case TokenSemicolon:
		return ";"
	case TokenPlusPlus:
		return "++"
	case TokenMinusMinus:
		return "--"
	case TokenPlusAssign:
		return "+="
	case TokenMinusAssign:
		return "-="
	case TokenPublic:
		return "public"
	case TokenPrivate:
		return "private"
	case TokenSpawn:
		return "spawn"
	case TokenChan:
		return "chan"
	case TokenCharKw:
		return "char"
	case TokenChar:
		return "char literal"
	case TokenAnnotation:
		return "annotation"
	case TokenWeak:
		return "weak"
	case TokenQuestion:
		return "?"
	case TokenNull:
		return "null"
	case TokenAmpersand:
		return "&"
	case TokenTry:
		return "try"
	case TokenCatch:
		return "catch"
	case TokenFinally:
		return "finally"
	case TokenThrow:
		return "throw"
	case TokenSwitch:
		return "switch"
	case TokenCase:
		return "case"
	case TokenDefault:
		return "default"
	case TokenEOF:
		return "end of file"
	default:
		return fmt.Sprintf("token(%d)", int(k))
	}
}

var Keywords = map[string]TokenKind{
	"fn":       TokenFn,
	"function": TokenFunction,
	"let":      TokenLet,
	"const":    TokenConst,
	"return":   TokenReturn,
	"if":       TokenIf,
	"else":     TokenElse,
	"while":    TokenWhile,
	"true":     TokenTrue,
	"false":    TokenFalse,
	"import":   TokenImport,
	"struct":   TokenStruct,
	"break":    TokenBreak,
	"continue": TokenContinue,
	"for":      TokenFor,
	"foreach":  TokenForeach,
	"as":       TokenAs,
	"int":      TokenIntKw,
	"bool":     TokenBool,
	"string":   TokenStringKw,
	"long":     TokenLong,
	"double":   TokenDouble,
	"void":     TokenVoid,
	"public":   TokenPublic,
	"private":  TokenPrivate,
	"spawn":    TokenSpawn,
	"chan":      TokenChan,
	"char":     TokenCharKw,
	"weak":     TokenWeak,
	"null":     TokenNull,
	"try":      TokenTry,
	"catch":    TokenCatch,
	"finally":  TokenFinally,
	"throw":    TokenThrow,
	"switch":   TokenSwitch,
	"case":     TokenCase,
	"default":  TokenDefault,
}
