package token

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
	TokenEOF
)

type Token struct {
	Kind  TokenKind
	Value string
	Line  int
	Col   int
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
}
