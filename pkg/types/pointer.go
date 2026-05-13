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
