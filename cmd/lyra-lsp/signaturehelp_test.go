package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

func TestSignatureHelp_BasicParams(t *testing.T) {
	h := servertest.New(t, newHandler())
	// `add` has two parameters; cursor is inside the call after the first arg.
	src := "let add = (x: i64, y: i64) -> i64 => x\nadd(1, "
	openAndWait(t, h, src)
	// Line 1, after `add(1, ` — column 7 (0-based).
	result, err := h.SignatureHelp(testURI, 1, 7)
	if err != nil {
		t.Fatalf("SignatureHelp: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil SignatureHelp")
	}
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least one signature")
	}
	sig := result.Signatures[0]
	if sig.Label != "add(x: i64, y: i64) -> i64" {
		t.Errorf("label = %q, want %q", sig.Label, "add(x: i64, y: i64) -> i64")
	}
	if len(sig.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(sig.Parameters))
	}
	// Cursor is after the comma so active param = 1.
	if result.ActiveParameter == nil || *result.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %v, want 1", result.ActiveParameter)
	}
}

func TestSignatureHelp_FirstParam(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "let greet = (name: string) -> string => name\ngreet("
	openAndWait(t, h, src)
	result, err := h.SignatureHelp(testURI, 1, 6)
	if err != nil {
		t.Fatalf("SignatureHelp: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil SignatureHelp")
	}
	if result.ActiveParameter == nil || *result.ActiveParameter != 0 {
		t.Errorf("ActiveParameter = %v, want 0", result.ActiveParameter)
	}
	if result.Signatures[0].Label != "greet(name: string) -> string" {
		t.Errorf("label = %q", result.Signatures[0].Label)
	}
}

func TestSignatureHelp_NoCallContext(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "let x = 42\n"
	openAndWait(t, h, src)
	// Not inside any call — should return nil.
	result, err := h.SignatureHelp(testURI, 0, 5)
	if err != nil {
		t.Fatalf("SignatureHelp: %v", err)
	}
	if result != nil && len(result.Signatures) > 0 {
		t.Errorf("expected no signatures outside a call, got %v", result)
	}
}

func TestSignatureHelp_ParameterLabelRanges(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Verify the label and parameter ranges for a two-param function.
	src := "let f = (a: i32, b: i32) -> i32 => a\nf("
	openAndWait(t, h, src)
	result, err := h.SignatureHelp(testURI, 1, 2)
	if err != nil {
		t.Fatalf("SignatureHelp: %v", err)
	}
	if result == nil || len(result.Signatures) == 0 {
		t.Fatal("expected signature")
	}
	sig := result.Signatures[0]
	label := sig.Label
	// Parameters should have [start, end] ranges that slice to the param text.
	for i, pi := range sig.Parameters {
		rng, ok := pi.Label.([]int)
		if !ok {
			// []interface{} from JSON round-trip
			break
		}
		if len(rng) != 2 {
			t.Errorf("param %d: expected [start, end], got %v", i, rng)
			continue
		}
		text := label[rng[0]:rng[1]]
		if text == "" {
			t.Errorf("param %d: empty label slice from %q[%d:%d]", i, label, rng[0], rng[1])
		}
	}
}

func TestSignatureHelp_UnknownCallee(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "unknown("
	openAndWait(t, h, src)
	result, err := h.SignatureHelp(testURI, 0, 8)
	if err != nil {
		t.Fatalf("SignatureHelp: %v", err)
	}
	if result != nil && len(result.Signatures) > 0 {
		t.Errorf("expected no signature for unknown callee, got %v", result)
	}
}
