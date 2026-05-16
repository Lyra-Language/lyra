package typechecker_test

import (
	"testing"
)

func TestStringConcat(t *testing.T) {
	res := parseCollectAndCheck(t, `let str = "hello" ++ "world"`, true)
	assertNoErrors(t, res)
}
