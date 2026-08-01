package llvm

import (
	"strings"
	"testing"
)

// A function value is a boxed closure `{ i8* fn, i8* env }` (closures.go). These
// tests drive the whole shape end to end: a lambda becomes a value, is passed and
// returned and stored, captures its enclosing bindings, and is called back
// through the one indirect-call convention — with the observable answer being the
// program's exit code.
func TestExec_Closures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// A *named* function used as a value. It captures nothing, so it flows
		// through the same fat pointer as any closure — via a thunk that adapts it
		// to the environment-taking convention.
		{
			"named function passed as a value",
			`let double = (x: i64) -> i64 => x * 2
			 let apply = (f: (i64) -> i64, x: i64) -> i64 => f(x)
			 let main = () -> u8 => u8(apply(double, 3))`,
			6,
		},
		// A lambda bound locally and called by name: the binding holds a closure
		// value, so this is an indirect call, not a direct one.
		{
			"local lambda called by name",
			`let main = () -> u8 => {
			   let f = (x: i64) -> i64 => x * 2
			   u8(f(3))
			 }`,
			6,
		},
		// The base case for capture: the body reads an enclosing local.
		{
			"captures an enclosing local",
			`let main = () -> u8 => {
			   let n = 5
			   let addN = (x: i64) -> i64 => x + n
			   u8(addN(3))
			 }`,
			8,
		},
		// The case that makes capture *necessary*: the closure outlives the frame
		// its captured binding lived in. By-value capture is what keeps this valid —
		// a reference to `n`'s slot would dangle the moment makeAdder returned.
		{
			"a returned closure outlives its frame",
			`let makeAdder = (n: i64) -> (i64) -> i64 => (x: i64) -> i64 => x + n
			 let main = () -> u8 => {
			   let add5 = makeAdder(5)
			   u8(add5(3))
			 }`,
			8,
		},
		// A lambda literal written straight into an argument, capturing as it goes.
		{
			"lambda literal as an argument",
			`let apply = (f: (i64) -> i64, x: i64) -> i64 => f(x)
			 let main = () -> u8 => {
			   let k = 10
			   u8(apply((x: i64) -> i64 => x + k, 5))
			 }`,
			15,
		},
		// The same function value called twice inside the callee.
		{
			"called twice through a parameter",
			`let twice = (f: (i64) -> i64, x: i64) -> i64 => f(f(x))
			 let main = () -> u8 => {
			   let n = 3
			   u8(twice((y: i64) -> i64 => y + n, 1))
			 }`,
			7,
		},
		// A closure stored in a struct field: `h.run(5)` is an indirect call through
		// the field, not a method dispatch — the two are told apart by what the
		// field is, since a builtin method call has the same syntax.
		{
			"closure in a struct field",
			`struct Handler { run: (i64) -> i64, id: u8 }
			 let main = () -> u8 => {
			   let bump = 2
			   let h = Handler { run: (x: i64) -> i64 => x + bump, id: 1 }
			   u8(h.run(5))
			 }`,
			7,
		},
		// An array of function values, called through an index.
		{
			"array of closures",
			`let main = () -> u8 => {
			   let fs: [2](i64) -> i64 = [(x: i64) -> i64 => x + 1, (x: i64) -> i64 => x * 2]
			   u8(fs[1](5))
			 }`,
			10,
		},
		// Several captures at once, of different widths — the environment lays them
		// out in a stable (name-sorted) order.
		{
			"several captures of different widths",
			`let main = () -> u8 => {
			   let a: u8 = 1
			   let b: i64 = 20
			   let c = true
			   let f = () -> i64 => if c { b + i64(a) } else { 0 }
			   u8(f())
			 }`,
			21,
		},
		// A capture reaching through an intermediate closure: the outer one must
		// capture `n` as well, or the inner has nothing to copy from.
		{
			"capture through a nested closure",
			`let main = () -> u8 => {
			   let n = 5
			   let outer = () -> (i64) -> i64 => (x: i64) -> i64 => x + n
			   let inner = outer()
			   u8(inner(1))
			 }`,
			6,
		},
		// A void-returning closure, called for effect through a parameter.
		{
			"void closure",
			`let run = (f: () -> void) -> u8 => { f(); 7 }
			 let main = () -> u8 => run(() -> void => { })`,
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A closure's environment is a **ref-counted box**, so a closure is a managed
// value like a string: copying one shares the environment, and the last reference
// frees it. Captured managed values are owned by the environment — retained when
// it is built, released by its drop glue — which is what keeps a captured string
// alive for exactly as long as the closure that captured it.
func TestExec_ClosureCaptureIsManaged(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{
			// The captured string outlives the frame it was created in: makeGreeter
			// returns, and the closure still holds a live reference to `name`.
			"captured string escapes with the closure",
			`let makeGreeter = (name: string) -> () -> string => () -> string => "hi " ++ name
			 let main = () -> u8 => {
			   let g = makeGreeter("bob")
			   println(g())
			   0
			 }`,
			"hi bob\n",
		},
		{
			// A heap string (not a literal, so a real box) captured and read back.
			"captures a heap string",
			`let main = () -> u8 => {
			   let s = "a" ++ "b"
			   let f = () -> string => s
			   println(f())
			   0
			 }`,
			"ab\n",
		},
		{
			// A void closure capturing a string, called through a parameter.
			"captured string used for effect",
			`let run = (f: () -> void) -> u8 => { f(); 0 }
			 let main = () -> u8 => {
			   let msg = "hello" ++ ""
			   run(() -> void => println(msg))
			 }`,
			"hello\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 || out != c.wantOut {
				t.Errorf("%s: exited %d with stdout %q; want 0 and %q", c.name, code, out, c.wantOut)
			}
			if got := buildAndRunASan(t, clang, c.src); got != 0 {
				t.Errorf("%s: ASan run exited %d; want 0", c.name, got)
			}
		})
	}
}

// The environment's own reference on a captured managed value, pinned by
// counting: `alloc + retain == release`.
//
// This is the assertion that matters, and ASan is not a substitute for it. Drop
// the retain and the box is released twice — once by the enclosing binding at
// scope exit, once by the environment's drop glue — which is a genuine
// double-free, yet the program still runs clean under ASan (the second release
// reads a refcount out of freed memory, gets a poison pattern rather than 1, and
// so never reaches the second free). The counts see it immediately.
func TestEmit_ClosureCaptureRetainAccounting(t *testing.T) {
	t.Parallel()
	// One allocation for the string, one for the environment; one retain as the
	// environment takes its own reference on the captured string; three releases —
	// the binding, the closure, and the string inside the environment's glue.
	src := `let main = () -> u8 => {
	   let s = "a" ++ "b"
	   let f = () -> string => s
	   if f() == "ab" { 0 } else { 1 }
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	allocs := strings.Count(ir, "call i8* @lyra_rc_alloc")
	retains := strings.Count(ir, "call void @lyra_rc_retain")
	releases := strings.Count(ir, "call void @lyra_rc_release")
	if allocs != 2 {
		t.Errorf("expected 2 allocations (the string and the environment), got %d", allocs)
	}
	if allocs+retains != releases {
		t.Errorf("conservation: %d allocations + %d retains != %d releases — a captured managed value needs the environment's own +1",
			allocs, retains, releases)
	}
}

// A closure returning a managed value transfers a fresh reference to its caller,
// exactly as a named function does. The ownership pass reads that convention off
// the callee's static function type: an indirect call has no LambdaExpr to
// resolve, and treating it as an *unknown* callee — whose result is conservatively
// borrowed — silently leaked the returned string at every call.
func TestEmit_ClosureReturnedValueIsReleased(t *testing.T) {
	t.Parallel()
	src := `let make = () -> () -> string => () -> string => "a" ++ "b"
	 let main = () -> u8 => {
	   let f = make()
	   if f() == "ab" { 0 } else { 1 }
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	// Counted inside @main specifically: two releases, the returned temporary and
	// the closure binding itself. A whole-module count would not distinguish them,
	// and would be muddied by the pinned (no-op) release of a captureless
	// environment — a site that costs nothing at run time.
	if got := releasesIn(ir, "@main"); got != 2 {
		t.Errorf("@main has %d releases; want 2 (the returned string temporary and the closure)\n%s", got, ir)
	}
	if got := buildAndRun(t, src); got != 0 {
		t.Errorf("exited %d; want 0", got)
	}
}

// releasesIn counts lyra_rc_release calls inside one function definition, so an
// assertion can be about *where* a release lands rather than how many the module
// contains in total.
func releasesIn(ir, fnName string) int {
	start := strings.Index(ir, "define ")
	for start >= 0 {
		header := ir[start:]
		end := strings.Index(header, "\n}")
		if end < 0 {
			return 0
		}
		body := header[:end]
		if strings.Contains(strings.SplitN(body, "(", 2)[0], fnName) {
			return strings.Count(body, "call void @lyra_rc_release")
		}
		next := strings.Index(ir[start+1:], "\ndefine ")
		if next < 0 {
			return 0
		}
		start += next + 1
	}
	return 0
}

// The representation, pinned in the IR. Three claims that the exec tests above
// cannot distinguish: the value really is a two-pointer pair, the lifted body
// really does take its environment first, and a **captureless** closure really
// does cost no allocation.
func TestEmit_ClosureRepresentation(t *testing.T) {
	t.Parallel()

	t.Run("captureless closures allocate nothing", func(t *testing.T) {
		t.Parallel()
		got, err := emitSource(t, `let double = (x: i64) -> i64 => x * 2
		 let apply = (f: (i64) -> i64, x: i64) -> i64 => f(x)
		 let main = () -> u8 => u8(apply(double, 3))`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "call i8* @lyra_rc_alloc") {
			t.Errorf("a captureless function value should share the pinned empty environment:\n%s", got)
		}
		for _, want := range []string{
			"@.closure.empty_env", // the shared pinned environment
			"@double.closure(",    // the thunk adapting the named function
		} {
			if !strings.Contains(got, want) {
				t.Errorf("expected IR to contain %q; got:\n%s", want, got)
			}
		}
	})

	t.Run("a capturing closure allocates its environment", func(t *testing.T) {
		t.Parallel()
		got, err := emitSource(t, `let main = () -> u8 => {
		   let n = 5
		   let addN = (x: i64) -> i64 => x + n
		   u8(addN(3))
		 }`)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"call i8* @lyra_rc_alloc",              // the environment box
			"define i64 @lyra_closure_1(i8* %env,", // the lifted body, environment first
			"call void @lyra_rc_release",           // and it is freed
		} {
			if !strings.Contains(got, want) {
				t.Errorf("expected IR to contain %q; got:\n%s", want, got)
			}
		}
	})

	t.Run("a function type lowers to the fat pointer", func(t *testing.T) {
		t.Parallel()
		got, err := emitSource(t, `let apply = (f: (i64) -> i64, x: i64) -> i64 => f(x)
		 let main = () -> u8 => 0`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "define i64 @lyra.apply({ i8*, i8* } %f,") {
			t.Errorf("a function-typed parameter should be { i8*, i8* }; got:\n%s", got)
		}
	})

	t.Run("a direct call keeps its plain signature", func(t *testing.T) {
		t.Parallel()
		// The thunk exists only for a function used as a *value*; calling one by
		// name must not grow an environment parameter, or every existing call site
		// (and its IR) would change for a feature it does not use.
		got, err := emitSource(t, `let double = (x: i64) -> i64 => x * 2
		 let main = () -> u8 => u8(double(3))`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "define i64 @lyra.double(i64 ") {
			t.Errorf("a named function should keep its plain signature; got:\n%s", got)
		}
		if strings.Contains(got, "@double.closure") {
			t.Errorf("no thunk should be emitted for a function never used as a value:\n%s", got)
		}
	})
}

// The drop glue is reached through one generic trampoline stored *in* the
// environment, because a release site knows only the closure's static type and
// never which lambda produced the value. A closure whose captures own nothing
// stores a null there, so the trampoline is a no-op for it.
func TestEmit_ClosureEnvironmentDropGlue(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let main = () -> u8 => {
	   let s = "a" ++ "b"
	   let f = () -> string => s
	   if f() == "ab" { 0 } else { 1 }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"define void @lyra_closure_env_drop(i8* %payload)", // the trampoline
		"define void @lyra_env_drop_1(i8* %payload)",       // the per-capture-set glue
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}

	// A closure capturing only scalars needs no glue at all.
	plain, err := emitSource(t, `let main = () -> u8 => {
	   let n = 5
	   let f = () -> i64 => n
	   u8(f())
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "@lyra_env_drop_") {
		t.Errorf("a scalar-only capture set should generate no drop glue:\n%s", plain)
	}
}

// TestExec_CaptureUsedOnlyThroughTupleIndex: a capture whose only use in the body is a
// tuple index (`p.0`) must still be captured. It was not — `ast.walkExprChildren` had no
// case for `*ast.TupleIndexExpr`, so the captures pass never descended into the indexed
// object, the environment got no slot for `p`, and lowering died with "unbound identifier
// \"p\"". A hard build failure on a correct program, and one that vanished the moment the
// body mentioned `p` any other way, which is what kept it hidden. The walker case fixes
// several passes at once (see checker/tuple_index_use_test.go); this is the one whose
// symptom was a failed build rather than a missing diagnostic.
func TestExec_CaptureUsedOnlyThroughTupleIndex(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `let mk = (p: (i64, i64)) -> () -> i64 => () -> i64 => p.0 + p.1
	 let main = () -> u8 => {
	   let f = mk((3, 4))
	   u8(f())
	 }`)
	if got != 7 {
		t.Errorf("exited %d; want 7", got)
	}
}
