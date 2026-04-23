package expressions

import (
	"strings"
	"testing"
)

func TestUnescapeStringContent_UppercaseUnicodeEscapeRejectsOutOfRangeCodePoint(t *testing.T) {
	_, err := unescapeStringContent(`\UFFFFFFFF`)
	if err == nil {
		t.Fatal("expected error for out-of-range \\U escape, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out-of-range error, got: %v", err)
	}
}

func TestUnescapeStringContent_UppercaseUnicodeEscapeAcceptsMaxUnicodeCodePoint(t *testing.T) {
	got, err := unescapeStringContent(`\U0010FFFF`)
	if err != nil {
		t.Fatalf("unexpected error for max valid code point: %v", err)
	}
	if got != string(rune(0x10FFFF)) {
		t.Fatalf("expected U+10FFFF, got %q", got)
	}
}
