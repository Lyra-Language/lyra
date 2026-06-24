package collector_test

import (
	"testing"
	"time"
)

// TestCollectZeroParamLambdaType_DoesNotHang is a regression test for a real
// bug: a zero-parameter lambda type (`() -> string`) omits the optional
// `parameter_types` field entirely (tree-sitter doesn't emit a node for it),
// so `ChildByFieldName` returns nil. The collector's parseParameterTypes used
// to call accessor methods (ChildCount, Child) directly on that nil node —
// which hangs inside the go-tree-sitter CGO binding instead of panicking, so
// a regression here would stall the whole test run rather than fail fast.
// Run on a goroutine with an explicit short deadline so a regression reports
// "this test hung" immediately instead of stalling for the default 10-minute
// `go test` timeout.
func TestCollectZeroParamLambdaType_DoesNotHang(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		source := `struct Box {
	show: () -> string
}`
		_, _, _, errs := parseAndCollect(t, source)
		if len(errs) != 0 {
			t.Errorf("expected no collector errors, got %v", errs)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("collecting a zero-parameter lambda type hung — regression in parseParameterTypes' nil handling")
	}
}

// TestCollectLambdaTypeWithParams_StillWorks: the non-empty case (which
// always had a real parameter_types node) keeps working after the nil guard.
func TestCollectLambdaTypeWithParams_StillWorks(t *testing.T) {
	source := `struct Box {
	combine: (i64, i64) -> i64
}`
	_, _, _, errs := parseAndCollect(t, source)
	if len(errs) != 0 {
		t.Errorf("expected no collector errors, got %v", errs)
	}
}
