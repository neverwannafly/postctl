package response

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/shubham/postctl/internal/ui"
)

func highlightLine(t ui.Theme, lang, line string) string {
	if t.NoColor {
		return line
	}
	var l chroma.Lexer
	switch lang {
	case "json":
		l = lexers.Get("json")
	case "xml":
		l = lexers.Get("xml")
	case "html":
		l = lexers.Get("html")
	default:
		return t.Render(ui.TextPrimary, line)
	}
	if l == nil {
		return t.Render(ui.TextPrimary, line)
	}
	it, err := l.Tokenise(nil, line)
	if err != nil {
		return t.Render(ui.TextPrimary, line)
	}
	var b strings.Builder
	for {
		tok := it()
		if tok == chroma.EOF {
			break
		}
		b.WriteString(t.Render(chromaToken(tok.Type), tok.Value))
	}
	return b.String()
}

func chromaToken(tt chroma.TokenType) ui.Token {
	switch tt {
	case chroma.Keyword, chroma.KeywordConstant, chroma.KeywordDeclaration:
		return ui.SyntaxKey
	case chroma.String, chroma.StringAffix, chroma.StringChar, chroma.StringDelimiter,
		chroma.StringDoc, chroma.StringDouble, chroma.StringEscape, chroma.StringHeredoc,
		chroma.StringInterpol, chroma.StringOther, chroma.StringRegex, chroma.StringSingle,
		chroma.StringSymbol:
		return ui.SyntaxString
	case chroma.Number, chroma.NumberBin, chroma.NumberFloat, chroma.NumberHex,
		chroma.NumberInteger, chroma.NumberOct:
		return ui.SyntaxNumber
	case chroma.NameBuiltin, chroma.Literal, chroma.LiteralDate:
		return ui.SyntaxBool
	case chroma.NameConstant:
		return ui.SyntaxNull
	default:
		return ui.TextPrimary
	}
}

func highlightLang(kind BodyKind) string {
	switch kind {
	case BodyJSON:
		return "json"
	case BodyXML:
		return "xml"
	case BodyHTML:
		return "html"
	default:
		return ""
	}
}
