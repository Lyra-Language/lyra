package printer

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// PrintAST walks any AST/type value and returns a stable, human-readable tree.
// It is intentionally generic so node definitions can stay data-focused.
func PrintAST(root any) string {
	w := &astWalker{
		sb:      &strings.Builder{},
		visited: map[visitKey]struct{}{},
	}
	w.writeValue(reflect.ValueOf(root), "")
	return w.sb.String()
}

// PrintASTWithLocations is PrintAST with each node's source span, as a `Loc:` line
// directly under its header.
//
// **A second mode rather than a second printer**, and off by default: the 245 golden files
// are dumps of the default one, and a span moves whenever a *test's source* is reindented,
// which would make every golden a diff about whitespace. The embedded `AstBase` is skipped
// for exactly that reason (exportedFields), so a location is unreachable in the default
// mode and unreachable to anything checking against it.
//
// It exists for the bootstrap: the Lyra collector has to set a location on every node, and
// no golden can say whether it set the right one. This is that oracle — both collectors
// printed this way agree or they do not.
//
// `File` is deliberately left out. The two collectors are handed the same source through
// different paths, so the path is the one part of a Location that legitimately differs.
func PrintASTWithLocations(root any) string {
	w := &astWalker{
		sb:            &strings.Builder{},
		visited:       map[visitKey]struct{}{},
		showLocations: true,
	}
	w.writeValue(reflect.ValueOf(root), "")
	return w.sb.String()
}

type visitKey struct {
	ptr uintptr
	typ reflect.Type
}

type astWalker struct {
	sb      *strings.Builder
	visited map[visitKey]struct{}
	// showLocations prints each node's span; see PrintASTWithLocations.
	showLocations bool
}

func (w *astWalker) line(indent, s string) {
	w.sb.WriteString(indent)
	w.sb.WriteString(s)
	w.sb.WriteString("\n")
}

func (w *astWalker) writeValue(v reflect.Value, indent string) {
	if !v.IsValid() {
		w.line(indent, "nil")
		return
	}

	// Unwrap interfaces.
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			w.line(indent, "nil")
			return
		}
		v = v.Elem()
	}

	// Handle pointers with cycle detection.
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			w.line(indent, "nil")
			return
		}
		key := visitKey{ptr: v.Pointer(), typ: v.Type()}
		if _, seen := w.visited[key]; seen {
			w.line(indent, "<cycle>")
			return
		}
		w.visited[key] = struct{}{}
		defer delete(w.visited, key)
		w.writeValue(v.Elem(), indent)
		return
	}

	switch v.Kind() {
	case reflect.Struct:
		w.writeStruct(v, indent)
	case reflect.Slice, reflect.Array:
		w.writeSlice(v, indent)
	case reflect.String:
		w.line(indent, v.String())
	case reflect.Bool:
		w.line(indent, fmt.Sprintf("%t", v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		w.line(indent, fmt.Sprintf("%d", v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		w.line(indent, fmt.Sprintf("%d", v.Uint()))
	case reflect.Float32, reflect.Float64:
		w.line(indent, fmt.Sprintf("%v", v.Float()))
	default:
		w.line(indent, fmt.Sprintf("%v", v.Interface()))
	}
}

func (w *astWalker) writeStruct(v reflect.Value, indent string) {
	t := v.Type()

	name := t.Name()
	if name == "" {
		name = t.String()
	}

	// Preserve familiar top-level shape for program nodes.
	if name == "Program" {
		statements := v.FieldByName("Statements")
		if statements.IsValid() && statements.Kind() == reflect.Slice {
			w.line(indent, fmt.Sprintf("Program(%d statements) {", statements.Len()))
		} else {
			w.line(indent, "Program {")
		}
	} else if label := w.nameLabel(v); label != "" {
		w.line(indent, fmt.Sprintf("%s(%s) {", name, label))
	} else {
		w.line(indent, fmt.Sprintf("%s {", name))
	}

	if w.showLocations {
		if loc, ok := locationOf(v); ok {
			w.line(indent+"\t", "Loc: "+loc)
		}
	}

	fields := exportedFields(v)
	for _, f := range fields {
		fieldValue := v.FieldByIndex(f.Index)
		if isZeroValue(fieldValue) {
			continue
		}
		w.writeField(f.Name, fieldValue, indent+"\t")
	}

	w.line(indent, "}")
}

func (w *astWalker) writeSlice(v reflect.Value, indent string) {
	w.line(indent, "{")
	for i := 0; i < v.Len(); i++ {
		w.writeValue(v.Index(i), indent+"\t")
	}
	w.line(indent, "}")
}

func (w *astWalker) writeField(name string, v reflect.Value, indent string) {
	kind := v.Kind()
	if kind == reflect.Interface && !v.IsNil() {
		v = v.Elem()
		kind = v.Kind()
	}
	if kind == reflect.Ptr && v.IsNil() {
		w.line(indent, fmt.Sprintf("%s: nil", name))
		return
	}
	if isEmptyComposite(v) {
		return
	}

	switch kind {
	case reflect.Slice, reflect.Array:
		w.line(indent, fmt.Sprintf("%s: {", name))
		for i := 0; i < v.Len(); i++ {
			w.writeValue(v.Index(i), indent+"\t")
		}
		w.line(indent, "}")
	case reflect.Struct, reflect.Ptr, reflect.Interface:
		w.line(indent, fmt.Sprintf("%s: {", name))
		w.writeValue(v, indent+"\t")
		w.line(indent, "}")
	default:
		w.line(indent, fmt.Sprintf("%s: %v", name, v.Interface()))
	}
}

func (w *astWalker) nameLabel(v reflect.Value) string {
	// Prefer explicit Name field when present.
	nameField := v.FieldByName("Name")
	if nameField.IsValid() && nameField.Kind() == reflect.String && nameField.String() != "" {
		return nameField.String()
	}

	// Fallback to GetName() when the method exists.
	method := v.MethodByName("GetName")
	if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() == 1 && method.Type().Out(0).Kind() == reflect.String {
		out := method.Call(nil)[0].String()
		if out != "" {
			return out
		}
	}
	return ""
}

// locationOf reads a node's span through the embedded base exportedFields skips, rendered
// as `line:col-line:col`. False for a type that embeds no base — `MatchArm`, `GenericParam`
// and the instance's `StructField` are payloads rather than nodes and carry no position,
// which is a distinction the bootstrap has to mirror rather than guess at.
func locationOf(v reflect.Value) (string, bool) {
	if v.Kind() != reflect.Struct {
		return "", false
	}
	f := v.FieldByName("Location")
	if !f.IsValid() || f.Type().Name() != "Location" {
		return "", false
	}
	start := f.FieldByName("StartLine")
	if !start.IsValid() {
		return "", false
	}
	return fmt.Sprintf("%d:%d-%d:%d",
		f.FieldByName("StartLine").Int(), f.FieldByName("StartCol").Int(),
		f.FieldByName("EndLine").Int(), f.FieldByName("EndCol").Int()), true
}

func exportedFields(v reflect.Value) []reflect.StructField {
	t := v.Type()
	fields := make([]reflect.StructField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Skip unexported fields and embedded base structs used only for location/markers.
		if f.PkgPath != "" {
			continue
		}
		if f.Anonymous && (f.Name == "AstBase" || f.Name == "ExprBase" || f.Name == "PatternBase") {
			continue
		}
		// Fields tagged `print:"-"` (e.g. NameLocation) carry auxiliary source
		// positions for tooling, not structural content; omit them so goldens
		// stay stable under unrelated edits.
		if f.Tag.Get("print") == "-" {
			continue
		}
		fields = append(fields, f)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Bool:
		return !v.Bool()
	case reflect.String:
		return v.Len() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Ptr, reflect.Interface, reflect.Func:
		return v.IsNil()
	case reflect.Slice, reflect.Map:
		return v.IsNil() || v.Len() == 0
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !isZeroValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		zero := reflect.Zero(v.Type())
		return reflect.DeepEqual(v.Interface(), zero.Interface())
	default:
		return false
	}
}

func isEmptyComposite(v reflect.Value) bool {
	for v.IsValid() && v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}

	for v.IsValid() && v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}

	if !v.IsValid() {
		return false
	}

	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return v.Len() == 0
	case reflect.Struct:
		fields := exportedFields(v)
		if len(fields) == 0 {
			return false // unit type — its presence is meaningful
		}
		for _, f := range fields {
			fieldValue := v.FieldByIndex(f.Index)
			if isZeroValue(fieldValue) || isEmptyComposite(fieldValue) {
				continue
			}
			return false
		}
		return true
	default:
		return false
	}
}
