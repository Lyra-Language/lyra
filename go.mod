module github.com/Lyra-Language/lyra

go 1.25.4

require (
	github.com/Lyra-Language/tree-sitter-lyra v0.0.0
	github.com/llir/llvm v0.3.6
	github.com/tree-sitter/go-tree-sitter v0.25.0
)

require (
	github.com/mewmew/float v0.0.0-20201204173432-505706aa38fa // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/mod v0.4.2 // indirect
	golang.org/x/sys v0.0.0-20210510120138-977fb7262007 // indirect
	golang.org/x/tools v0.1.4 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
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
