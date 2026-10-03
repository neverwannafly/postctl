package vimtext

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/shubham/postctl/internal/ui"
)

type chromaHighlighter struct {
	th    ui.Theme
	lexer chroma.Lexer
}

// ChromaHighlighter returns a syntax highlighter using postctl semantic tokens.
func ChromaHighlighter(th ui.Theme, lexer string) Highlighter {
	return &chromaHighlighter{th: th, lexer: lexers.Get(lexer)}
}

// Highlight returns semantic Chroma spans.
func (h *chromaHighlighter) Highlight(lines []string) [][]Span {
	out := make([][]Span, len(lines))
	if h.th.NoColor || h.lexer == nil {
		return out
	}
	tokens, err := chroma.Tokenise(h.lexer, nil, strings.Join(lines, "\n"))
	if err != nil {
		return out
	}
	row, col := 0, 0
	for _, token := range tokens {
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if row >= len(out) {
				return out
			}
			if part != "" {
				out[row] = append(out[row], Span{
					Start: col,
					End:   col + len(part),
					Style: h.th.Style(chromaToken(token)),
				})
				col += len(part)
			}
			if i+1 < len(parts) {
				row++
				col = 0
			}
		}
	}
	return out
}

func chromaToken(token chroma.Token) ui.Token {
	switch {
	case token.Type.InCategory(chroma.Comment):
		return ui.TextQuiet
	case token.Type.InCategory(chroma.LiteralString):
		return ui.SyntaxString
	case token.Type.InCategory(chroma.LiteralNumber):
		return ui.SyntaxNumber
	case token.Type.InCategory(chroma.Name):
		return ui.SyntaxKey
	case token.Type.InCategory(chroma.Keyword):
		if token.Value == "true" || token.Value == "false" {
			return ui.SyntaxBool
		}
		if token.Value == "null" {
			return ui.SyntaxNull
		}
		return ui.AccentPrimary
	case token.Type.InCategory(chroma.Operator):
		return ui.TextSecondary
	default:
		return ui.TextPrimary
	}
}
