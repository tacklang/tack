package lexer

import "strconv"

// TokenKind classifies a token. The set covers Go-like syntax beyond what the
// parser accepts today, so the lexer can tokenize source the parser later
// rejects with a proper diagnostic.
type TokenKind int

// The token kinds. The trailing comment on an operator is its spelling.
const (
	// InvalidToken is the zero value and is never produced by the lexer.
	InvalidToken TokenKind = iota

	// EOF ends every token stream.
	EOF
	// Comment is a line comment, or a run of consecutive line comments.
	Comment

	// Ident through Str are identifiers, keywords, and literals.
	Ident
	Keyword
	Int
	Float
	Char
	Str

	// Add through Mod are arithmetic operators.
	Add // +
	Sub // -
	Mul // *
	Div // /
	Mod // %

	// BitAnd through ShiftRight are bitwise operators.
	BitAnd     // &
	BitOr      // |
	BitXor     // ^
	BitAndNot  // &^
	ShiftLeft  // <<
	ShiftRight // >>

	// Assign through ShiftRightAssign are assignment operators.
	Assign // =
	Define // :=

	AddAssign // +=
	SubAssign // -=
	MulAssign // *=
	DivAssign // /=
	ModAssign // %=

	BitAndAssign     // &=
	BitOrAssign      // |=
	BitXorAssign     // ^=
	BitAndNotAssign  // &^=
	ShiftLeftAssign  // <<=
	ShiftRightAssign // >>=

	// LogicalAnd through LogicalNot are logical operators.
	LogicalAnd // &&
	LogicalOr  // ||
	LogicalNot // !

	// Equal through GreaterEqual are comparison operators.
	Equal        // ==
	NotEqual     // !=
	Less         // <
	LessEqual    // <=
	Greater      // >
	GreaterEqual // >=

	// Inc, Dec and Arrow are increment, decrement and channel operators.
	Inc   // ++
	Dec   // --
	Arrow // <-

	// LParen through RCurl are brackets.
	LParen // (
	RParen // )
	LBrack // [
	RBrack // ]
	LCurl  // {
	RCurl  // }

	// Comma through Ellipsis are separators. Semi is also inserted
	// automatically at line ends; see addSemi.
	Comma    // ,
	Period   // .
	Colon    // :
	Semi     // ;
	Ellipsis // ...

	// Tilde is ~.
	Tilde // ~
)

var tokenNames = map[TokenKind]string{
	EOF:     "EOF",
	Comment: "comment",

	Ident:   "ident",
	Keyword: "keyword",
	Int:     "int",
	Float:   "float",
	Char:    "char",
	Str:     "string",

	Add: "+",
	Sub: "-",
	Mul: "*",
	Div: "/",
	Mod: "%",

	BitAnd:     "&",
	BitOr:      "|",
	BitXor:     "^",
	BitAndNot:  "&^",
	ShiftLeft:  "<<",
	ShiftRight: ">>",

	Assign: "=",
	Define: ":=",

	AddAssign: "+=",
	SubAssign: "-=",
	MulAssign: "*=",
	DivAssign: "/=",
	ModAssign: "%=",

	BitAndAssign:     "&=",
	BitOrAssign:      "|=",
	BitXorAssign:     "^=",
	BitAndNotAssign:  "&^=",
	ShiftLeftAssign:  "<<=",
	ShiftRightAssign: ">>=",

	LogicalAnd: "&&",
	LogicalOr:  "||",
	LogicalNot: "!",

	Equal:        "==",
	NotEqual:     "!=",
	Less:         "<",
	LessEqual:    "<=",
	Greater:      ">",
	GreaterEqual: ">=",

	Inc:   "++",
	Dec:   "--",
	Arrow: "<-",

	LParen: "(",
	RParen: ")",
	LBrack: "[",
	RBrack: "]",
	LCurl:  "{",
	RCurl:  "}",

	Comma:    ",",
	Period:   ".",
	Colon:    ":",
	Semi:     ";",
	Ellipsis: "...",

	Tilde: "~",
}

// String returns the token's spelling for operators, or a lowercase name for
// the other kinds.
func (k TokenKind) String() string {
	if s, ok := tokenNames[k]; ok {
		return s
	}
	return "TokenKind(" + strconv.Itoa(int(k)) + ")"
}
