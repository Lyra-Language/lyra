package types

import "fmt"

type RawPointerType struct {
	Pointee Type
	IsMut   bool
}

func (r RawPointerType) typeNode() {}
func (r RawPointerType) GetName() string {
	if r.IsMut {
		return fmt.Sprintf("^mut %s", r.Pointee.GetName())
	}
	return fmt.Sprintf("^%s", r.Pointee.GetName())
}
func (r RawPointerType) String() string { return r.GetName() }

// WeakType is a non-owning ("weak") reference to a managed value — the intended
// answer to reference-cycle leaks among `shared` values (a strong cycle never
// reaches refcount zero; a weak edge breaks it). Like a pointer it is
// pointer-sized, so a `weak` field also breaks the *size* cycle lyra-E014
// guards. It is deliberately NOT a `shared` allocation flavor: `AllocationOf` on
// it is Unspecified, so it is non-managed and never retained/released/dropped —
// which is the whole point of a weak reference. The non-owning runtime semantics
// (a separate weak count, upgrade-to-strong) are deferred until refcount codegen
// grows them; today `weak T` parses, type-checks, and lowers to a plain pointer
// for layout, but there is no way to construct a weak value yet.
type WeakType struct {
	Inner Type
}

func (w WeakType) typeNode() {}
func (w WeakType) GetName() string {
	// Inner may be nil on a malformed `weak` with no operand; render "?" so a
	// String() used to build an error message never panics (mirrors ReturnType).
	inner := "?"
	if w.Inner != nil {
		inner = w.Inner.GetName()
	}
	return fmt.Sprintf("weak %s", inner)
}
func (w WeakType) String() string { return w.GetName() }
