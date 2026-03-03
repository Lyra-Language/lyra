package collector

import (
	"bytes"
	"io"
	"os"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// captureProgramPrint runs program.Print("") with stdout redirected and returns the output.
// This lets tests assert on the full collected AST shape without manual type assertions.
func captureProgramPrint(program *ast.Program) string {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	old := os.Stdout
	os.Stdout = w
	program.Print("")
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
