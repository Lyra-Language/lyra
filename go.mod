module github.com/Lyra-Language/lyra

go 1.25.4

require (
	github.com/Lyra-Language/tree-sitter-lyra v0.0.0
	github.com/tree-sitter/go-tree-sitter v0.25.0
)

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/owenrumney/go-lsp v0.2.2
)

require (
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/sergi/go-diff v1.4.0
)

// Remove this replace directive once you've pushed and tagged a release
replace github.com/Lyra-Language/tree-sitter-lyra => ../tree-sitter-lyra
