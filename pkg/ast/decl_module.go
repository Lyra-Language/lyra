package ast

type ModuleDeclStmt struct {
	AstBase
	Path []ModuleName
	// Doc is the `//!` block at the top of the file, nil if there is none.
	//
	// It is `//!` rather than a `///` above the `module` line because a module is a
	// file **or a directory**: a directory module has no single header to sit above,
	// and its several files each want to say something about the module they join.
	// Docs from every file of a multi-file module are concatenated in file order.
	Doc *Doc
	// Links are the libraries `@link("m")` on the module header names.
	//
	// **Module-level because that is where the fact lives.** A binding module links one
	// library and declares a dozen `extern`s against it, so repeating `@link` on each was
	// the same claim written a dozen times — `bindings/sdl3` carried fourteen. The
	// per-extern form stays legal and is what a lone `extern` in a module-less program
	// uses; the driver takes the union of both.
	Links []string
}

func (m *ModuleDeclStmt) statementNode() {}

func (m *ModuleDeclStmt) GetName() string { return "module" }

type ModuleName struct {
	Name string
}
