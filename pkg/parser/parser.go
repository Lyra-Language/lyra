package parser

/*
#include <stddef.h>
void ts_set_allocator(void *(*)(size_t), void *(*)(size_t, size_t), void *(*)(void *, size_t), void (*)(void *));
static void lyra_libc_allocator(void) { ts_set_allocator(NULL, NULL, NULL, NULL); }
*/
import "C"

import (
	"errors"

	lyra_parser "github.com/Lyra-Language/tree-sitter-lyra/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// **tree-sitter allocates with libc's own functions**, not go-tree-sitter's. Its init hands
// tree-sitter allocators that call back into Go only to call libc's malloc there — C to Go
// to C for every node — which was four fifths of parsing Vega's sources, and over half of
// `lyrac check` on it (10/30). Nothing here sets an allocator of its own, so the memory is
// libc's either way, and go-tree-sitter's frees through C.free still match.
func init() {
	C.lyra_libc_allocator()
}

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
