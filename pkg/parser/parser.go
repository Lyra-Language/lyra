package parser

import (
	"errors"

	lyra_parser "github.com/Lyra-Language/tree-sitter-lyra/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func Parse(text string) (*sitter.Tree, error) {
	language := sitter.NewLanguage(lyra_parser.Language())
	if language == nil {
		return nil, errors.New("failed to load lyra grammar")
	}
	parser := sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		return nil, err
	}
	return parser.Parse([]byte(text), nil), nil
}
