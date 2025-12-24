module avrameisner.com/lyra-lsp

go 1.25.4

require (
	github.com/Lyra-Language/tree-sitter-lyra v0.0.0
	github.com/tree-sitter/go-tree-sitter v0.25.0
)

require github.com/mattn/go-pointer v0.0.1 // indirect

// Remove this replace directive once you've pushed and tagged a release
replace github.com/Lyra-Language/tree-sitter-lyra => ../tree-sitter-lyra
