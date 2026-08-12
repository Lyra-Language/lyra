package diagnostic

const (
	CodeTypeError                = "lyra-E001"
	CodeUseBeforeDeclaration     = "lyra-E002"
	CodeReturnOutsideFunction    = "lyra-E003"
	CodeBreakContinueOutsideLoop = "lyra-E004"
	CodeAwaitOutsideAsync        = "lyra-E005"
	CodeYieldOutsideGenerator    = "lyra-E006"
	CodePurityViolation          = "lyra-E007"
	CodeTryOutsideResult         = "lyra-E008"

	// CodeNonExhaustiveMatch: a match on a closed type (`bool` or a `data`/sum
	// type) leaves cases uncovered. Error rather than warning because the set of
	// cases is finite and known — the author can still write an explicit `_ =>`
	// to opt out. Open types (wide integers, strings) stay a warning.
	CodeNonExhaustiveMatch = "lyra-E009"

	// CodeUninitializedDeclaration: a `let`/`var` binding has no initializer.
	// Initialization is required at declaration so a binding can never be read
	// before assignment (no use-of-uninitialized). Allowing uninitialized `var`
	// behind a definite-assignment pass may be revisited later.
	CodeUninitializedDeclaration = "lyra-E010"

	// CodeUnsafeOutsideUnsafe: a raw-pointer operation (`&x`, `*p`, a pointer
	// write) or a call to an `unsafe` function appears outside an `unsafe` block
	// or `unsafe` function body. The unsafe surface must be explicit and loud.
	CodeUnsafeOutsideUnsafe = "lyra-E011"

	// CodeNonConstantConstInitializer: a `const` binding's initializer is not a
	// compile-time constant. A `const` must be evaluable at compile time, so its
	// initializer may only be a literal, another constant, or an expression built
	// purely from those (arithmetic, boolean, string concatenation, array/tuple
	// of constants). Anything depending on runtime state (function calls, non-const
	// variables, interpolation, etc.) is rejected.
	CodeNonConstantConstInitializer = "lyra-E012"

	// CodeMissingStructField: a struct literal omits a declared field that has no
	// default value. Carries its own code (rather than the generic type error) so
	// the LSP can offer an "Add missing struct fields" quick fix.
	CodeMissingStructField = "lyra-E013"

	// CodeRecursiveType: a struct, data, or named-tuple type contains itself
	// by value (directly or through a cycle of by-value references), giving it
	// unbounded size. The fix is to break the cycle with a `shared` modifier on
	// the type declaration or on the recursive field/constructor-parameter.
	CodeRecursiveType = "lyra-E014"

	// CodeConflictingEffectBounds: a function or method carries two mutually
	// exclusive correctness-axis bounds — `pure` and `det`. They sit on the same
	// axis and `pure` is the stronger guarantee (it already implies determinism),
	// so writing both is contradictory. `noalloc` is a separate resource axis and
	// stacks freely with either, so it is never part of this conflict.
	CodeConflictingEffectBounds = "lyra-E015"

	// CodeEffectBoundViolation: a function or method declared `det` performs a
	// non-deterministic effect (reads external input, randomness, or the clock),
	// or one declared `noalloc` heap-allocates. `pure`'s equivalent is the older
	// CodePurityViolation (E007); `det`/`noalloc` get their own code.
	CodeEffectBoundViolation = "lyra-E016"

	// CodeMalformedBuiltin: a `@builtin(X)` attribute is invalid — an unrecognized
	// builtin name, a target whose shape doesn't match the canonical kind it
	// claims (e.g. `@builtin(Result)` on a type without single-payload Ok/Err
	// constructors), or a second declaration claiming a kind already taken.
	CodeMalformedBuiltin = "lyra-E017"

	// CodeAllocationMismatch: a value is *owned* into a slot whose storage flavor
	// differs — a `shared` (heap, ref-counted) value stored where a `stack`
	// (inline, value-semantics) value is expected, or vice versa. Allocation is
	// not part of nominal identity (assignability otherwise ignores it), but
	// crossing the flavor boundary changes representation, so it must be an
	// explicit operation rather than a silent coercion. Only fires when both
	// sides carry a concrete, differing flavor; an unspecified flavor inherits
	// from context and is compatible with either. Borrowed (`ref`/`mut`)
	// parameters are allocation-polymorphic and are not subject to this check.
	CodeAllocationMismatch = "lyra-E018"

	// CodeUseAfterMove: a binding is read after its value was moved into an `own`
	// parameter. `own` means the callee takes ownership — it may release the value
	// or, under Perceus reuse, overwrite its box in place — so the caller must
	// treat the binding as consumed. Flow-sensitive and conservative at joins: a
	// move in either branch of an `if`/`match` counts as moved afterwards, and a
	// move anywhere in a loop body counts as moved on the next iteration.
	// Reassigning the binding gives it a fresh value and clears the move.
	CodeUseAfterMove = "lyra-E019"

	// CodeIntegerOverflow: an integer `+`/`-`/`*`/unary-`-` whose operand ranges
	// (tracked by the value-range analysis) prove the result cannot fit its type on
	// any path — a guaranteed runtime overflow trap, caught at compile time. Unlike
	// the literal range check (lyra-E001) this fires on *non-constant* variables
	// whose range is known from a branch refinement (`if x > 100 { x + 100 }` on an
	// i8). Only definite overflow is reported; a merely *possible* overflow is left
	// to the runtime trap.
	CodeIntegerOverflow = "lyra-E020"

	// CodeDivideByZero: an integer `/`/`%`/`%%` whose divisor's value range (tracked
	// by the value-range analysis) proves it is *always* zero on a reachable path —
	// a guaranteed runtime divide-by-zero trap, caught at compile time. The
	// error-reporting twin of the divide-by-zero trap *elision* (which fires when the
	// divisor is provably *non*-zero); symmetric with lyra-E020, and flow-sensitive
	// the same way (`if b == 0 { a / b }` catches the refined divisor). Only a
	// *definite* divide-by-zero is reported; a merely *possible* one (the divisor
	// range includes 0 but also nonzero values) is left to the runtime trap.
	CodeDivideByZero = "lyra-E021"

	// CodeIndexOutOfBounds: an array index `xs[i]` whose value range (tracked by the
	// value-range analysis) proves it is *always* out of bounds on a reachable path
	// — a guaranteed runtime bounds-trap, caught at compile time. The error-reporting
	// twin of the bounds-trap *elision* (which fires when the index is provably in
	// bounds); symmetric with lyra-E020/E021 and flow-sensitive the same way
	// (`if i >= size { xs[i] }` catches the refined index). Reported only for a
	// *non-singleton* index range entirely outside `[-size, size)` (a negative index
	// counts from the end): a single constant index is already the typechecker's own
	// range check (inferIndexExpr / resolveConstantInt), and a non-singleton range
	// guarantees that check didn't fire, so there is no double report. Only a
	// *definite* OOB is reported; a merely *possible* one is left to the runtime trap.
	CodeIndexOutOfBounds = "lyra-E022"

	// CodeRangeConstraintViolation: a compile-time numeric constant assigned to a
	// range-constrained newtype (`newtype Percent = u8 where range(0..<=100)`) falls
	// outside the declared range (`let p: Percent = 150`). The numeric analogue of
	// the string PatternConstraint check; checked for a foldable constant value (int
	// or float literal) against foldable literal bounds. A non-constant value is left
	// to the value-range analysis / runtime (not yet wired), so this is a
	// definite-only, compile-time check like the literal integer range check.
	CodeRangeConstraintViolation = "lyra-E023"

	// CodeValuesConstraintViolation: a compile-time literal assigned to a newtype with
	// a `values(...)` constraint is not one of them (`newtype Status = i32 where
	// values(200, 404, 500)` given `302`). The literal-union analogue of E023.
	//
	// Nothing enforced this until 08/12 — the constraint was collected, its shape
	// validated, and then read by nobody, so `values(...)` was a declaration the
	// compiler acknowledged and ignored. That is this project's recurring
	// collected-and-unread shape in the one place where being checked is the
	// declaration's entire purpose.
	CodeValuesConstraintViolation = "lyra-E045"

	// CodeImplicitNewtypeConversion: a value that already has a type used where a
	// newtype over that base is expected, without writing the conversion —
	// `take(plain_i64)` against `(c: Cents)`. An untyped *literal* is still adopted
	// implicitly (`let c: Cents = 150`); a typed value needs `Cents(x)`.
	//
	// The line is provenance, not convenience. A literal has no unit yet, so adopting
	// it costs nothing; a typed value came from somewhere, and that somewhere is where
	// a unit mixup lives. Until 08/12 base → newtype was assignable everywhere, so a
	// newtype declared a distinction the compiler then declined to enforce at any call
	// boundary — which also made lyra-E043 (the overflow-arithmetic refusal) narrower
	// than its own rationale, since the same laundering was available through any
	// user-written function.
	//
	// Ada's rule for derived types, and for its reason: `M : Meters := 3.0` is legal
	// because the literal is universal, `M := F` for a Float F is not, and `Meters(F)`
	// is the conversion.
	CodeImplicitNewtypeConversion = "lyra-E046"

	// CodeCapturedAssignment: a lambda assigns to a binding it captured from an
	// enclosing scope. A closure captures **by value** — the copy is taken when the
	// closure is created, so it can outlive the frame the original lives in — which
	// means the write can only reach the closure's own copy, never the enclosing
	// binding. Silently doing nothing is the same class of bug as a lost write
	// through a by-value `mut` parameter, so it is rejected instead: return the new
	// value, or move the state into a value the closure is handed.
	CodeCapturedAssignment = "lyra-E024"

	// CodeBorrowedParamReassignment: a function reassigns a *borrowed* parameter —
	// one with no modifier, or `ref`. A borrow is a view of a value someone else
	// owns, so rebinding the name can only affect the callee's own copy: the caller
	// sees nothing, exactly like the captured-assignment case above (E024) and the
	// by-value `mut` parameter that silently lost its writes.
	//
	// It is also inconsistent with the binding model. `let x = 5; x = 6` is an error
	// ("use 'var'"), yet a bare parameter accepted the same write, making a parameter
	// the most permissive rung with no syntax for the immutable one. `own` and `mut`
	// stay legal because for them the write means something: `own` transfers the value
	// to the callee, and `mut` is a reference to the caller's storage. Swift removed
	// `var` parameters for this same confusion (SE-0003); Rust requires opt-in and
	// keeps it local.
	//
	// The replacement is shadowing — `let s = s ++ "!"` — which says plainly that a
	// new local value is being made rather than the caller's being changed.
	CodeBorrowedParamReassignment = "lyra-E025"

	// CodeUnresolvedImport: an `import a.b` names a module no search root contains.
	// Module paths map to files by directory convention (`a.b` → `a/b.lyra`), so the
	// message lists the paths that were tried rather than only the name that failed.
	CodeUnresolvedImport = "lyra-E026"

	// CodeImportCycle: modules that import each other, directly or transitively.
	// Rejected rather than broken at an arbitrary edge: with no lazy or partial
	// initialization semantics defined, "which half of the cycle observes the other"
	// has no answer a user could predict.
	CodeImportCycle = "lyra-E027"

	// CodePrivateAccess: a reference to a name another module declared without `pub`.
	// Within a module everything is visible; `pub` is what crosses the boundary. The
	// message names the declaring module, since the fix is either to export the name
	// there or to stop reaching for it.
	CodePrivateAccess = "lyra-E028"

	// CodeMalformedModifiers: a callable's effect/behaviour modifiers are repeated or
	// written out of the canonical order (`unsafe pure|det noalloc async gen rec`).
	//
	// This used to be a *parse* error — the grammar spelled the modifiers as seven
	// ordered optionals. That shape was also the single largest thing in the generated
	// parser (`lambda_expr` owned 91% of its states; collapsing it to one repeated choice
	// took `parser.c` from 116 MB to 12.8 MB and out of Git LFS), so order and repetition
	// moved here. The diagnostic is strictly better for it: a syntax error pointed at
	// whichever token failed to shift, where this names the offending modifier and the
	// order to write.
	CodeMalformedModifiers = "lyra-E029"

	// CodeUnsupportedTraitBorrow is **retired** (08/03) and no longer emitted. The
	// number stays reserved rather than reused, so an old build's message and a search
	// for it still mean one thing.
	//
	// It rejected `own` on a trait method's parameter, because the ownership pass did
	// not analyze trait-method bodies at all — nothing recorded that a returned `own`
	// parameter was transferred rather than dropped, and the combination was a
	// heap-use-after-free under ASan (`take: (Self, own string) -> string`). Its own
	// doc named the condition for lifting it: teach `pkg/analyzer/ownership` about
	// method bodies. That landed with per-specialization method tables, together with
	// the other half nobody had written down — use-after-move could not resolve a
	// *method* callee either, so the caller's side of the transfer went unchecked.
	CodeUnsupportedTraitBorrow = "lyra-E030"

	// CodeUndeclaredTypeVariable: a signature mentions a type variable that the
	// binding's *written* generic parameter list does not declare — `let f<t> =
	// (a: u) -> u => a`.
	//
	// The list stays optional (type variables are lexical: a lowercase type name
	// is a variable wherever it appears, so `let unbox = (b: Box<t>, fb: t) -> t`
	// is generic with no list at all). Written, it is authoritative — which is
	// what gives a typo somewhere to be caught. Without this, a misspelled
	// lowercase type name does not fail: it silently becomes a *new* type
	// variable, and the function becomes generic in something its author never
	// meant. The signature still checks; what changes is that callers must now
	// solve a variable that should have been a fixed type, so the diagnostic (if
	// any) lands at a call site or surfaces only in the backend. That is how the
	// prelude's `ok`/`err` shipped without their `<t, e>` and drew nothing.
	//
	// Uppercase names never had this hole — an unknown one is an UnresolvedType
	// and is reported. This closes the lowercase half.
	CodeUndeclaredTypeVariable = "lyra-E031"

	// CodeMissingRangeEndOperator: a range pattern with an end bound but no end
	// operator — `0..9` rather than `0..<=9` or `0..<9`.
	//
	// The grammar accepts it, and every consumer of `RangePattern.EndOperator`
	// tests `== "<"`, so an empty operator fell through to *inclusive*: `0..9`
	// silently meant `0..<=9`. A token whose absence carries a defined-but-unwritten
	// meaning is what this language refuses everywhere else (wraparound explicit,
	// lossy conversions loud), and the default is not even the harmless kind — the
	// extra value is the difference between a `match` the exhaustiveness checker
	// calls complete and one it does not.
	//
	// Enforced here rather than in the grammar, following lyra-E029: a collector
	// diagnostic names the offending construct and both fixes, where a syntax error
	// would point at whichever token failed to shift. It also keeps the `..`
	// notation uniform across its three sites, so the *pattern* rule need not
	// differ from the expression rule in shape merely to express strictness.
	CodeMissingRangeEndOperator = "lyra-E032"

	// CodeInvalidRangeStep: a range step that cannot mean what it says — a step of
	// 0, or a fractional step over an integer domain.
	//
	// A step has two spellings: an expression range's `:step` (`0..<=100:2`) and a
	// `newtype`'s `step()` constraint (`range(0..<=100), step(0.25)`). They stay
	// separate on purpose — the constraint composes with `precision()` and the
	// newtype's domain, the expression drives a loop counter — but they must not
	// *mean* different things, and they did: the expression step was checked for
	// numeric type-compatibility only, and the constraint step was validated by
	// nothing at all. `types.InvalidStepReason` is now the one rule and both ask it.
	//
	// Type compatibility does not subsume this. `0..<=10:0` type-checks perfectly
	// and is a loop that cannot terminate.
	CodeInvalidRangeStep = "lyra-E033"

	// CodeDescendingRangeNotIterated: `..>` or `..>=` where a range is a **set** rather
	// than an iteration — a match pattern or a `newtype` range constraint.
	//
	// Direction is meaningful only when something walks the range. As a set, `5..>1`
	// describes exactly the members `1..<5` does, so a descending spelling is not a
	// different set but the same one written in a way that implies an order the construct
	// does not have.
	//
	// The grammar accepts all four operators at all three sites — one node kind, kept that
	// way by the 08/01 unification — and the restriction lives in the collector, which is
	// the line rangeBounds draws: the grammar refuses what has no meaning anywhere, the
	// collector refuses what has a plausible meaning needing disambiguation. Here the
	// author meant a set counted downwards, which the language does not have, so the
	// message names the ascending spelling of the same set rather than pointing at a token.
	CodeDescendingRangeNotIterated = "lyra-E034"

	// CodeTypeNameAsValue: a PascalCase name used in value position that is not a
	// data constructor — a type (`Rng.seeded(42)`), a trait (`Greet.hello(x)`), or
	// nothing at all (`Nonexistent.make(1)`).
	//
	// The collector reads every PascalCase name in expression position as a nullary
	// data constructor, since the lexer guarantees it is not a variable; when no
	// constructor owns it that reading is simply wrong. Answering nil and saying
	// nothing — which is what happened until 08/06 — let all three forms pass
	// `lyrac check` and fail in codegen as `llvm: unsupported method call`, the
	// backend refusing a form the front end never looked at rather than one it
	// accepted on purpose (hazard 5).
	//
	// The type case is the one worth naming precisely: Lyra has **no
	// type-namespaced associated functions**, so `Rng.seeded(42)` is not an
	// unimplemented call but a form the language does not have, and the free
	// function (`rng_seeded`) is the whole answer. That is also why the prelude's
	// constructors are spelled bare.
	CodeTypeNameAsValue = "lyra-E035"

	// CodeUnsatisfiedTraitBound: a generic is instantiated at a type that does not
	// satisfy its `where` bound — `describe(p)` on a `Pt` with no `Show` impl.
	//
	// The bounds were collected and never read until 08/07, so writing one bought
	// nothing: the call type-checked and died in the backend as
	// `llvm: unsupported method call`. Checked at the *instantiation* because that is
	// where the type variable first has a concrete type; the declaration cannot know
	// and the backend is too late.
	CodeUnsatisfiedTraitBound = "lyra-E036"

	// CodeDuplicateTraitImpl: two `impl <Trait> for <Type>` blocks name the same
	// trait and the same type, so which one a call dispatches to is decided by
	// declaration order rather than by the program.
	//
	// Accepted silently until 08/07, which looked harmless while a trait only *added*
	// methods: whichever impl won, the call had a body. It stops being harmless the
	// moment a trait **overrides** something — an `Eq` impl replacing structural
	// equality means two impls make `==` mean two things. It also already inverted
	// rule 5: `publishBoundCandidates` requires exactly one match, so a duplicated
	// impl published no candidate and surfaced as a *backend* error at a call site
	// far from the two declarations that caused it.
	CodeDuplicateTraitImpl = "lyra-E037"

	// CodeMalformedDerive: a `@derive(...)` the compiler cannot synthesize — a trait
	// it does not derive, or `@derive(Ord)` on something other than a struct with
	// fields. `@derive` parsed and was collected onto TypeDeclStmt.Derives from the
	// start and read by nobody, so an unsupported one compiled and silently did
	// nothing; naming it is what keeps it from being the next phantom builtin.
	CodeMalformedDerive = "lyra-E038"

	// CodeComparisonOperatorMethod: a trait method named for a comparison operator —
	// `(_==_)`, `(_<_)`, `(_<=>_)` and the rest.
	//
	// The compiler owns those seven as of 08/07: `==`/`!=` are structural and
	// overridden by the prelude's `Eq`, and `<`/`<=`/`>`/`>=`/`<=>` all derive from
	// `Ord::compare`. A second way to override them would be a coherence question with
	// no answer (which impl wins?), and declaring them separately reintroduces the
	// failure `Ord`'s single method exists to prevent — a type whose `<` and `<=>`
	// disagree, which is the C++/Java shape.
	CodeComparisonOperatorMethod = "lyra-E039"

	// CodeUnsatisfiedSupertrait: `impl B for T` where `trait B: A` and `T` has no
	// impl of `A`. A supertrait is a promise that every `B` is also an `A`, which is
	// what lets a `where t: B` bound call `A`'s methods.
	//
	// `TraitDeclStmt.Bounds` was collected and read by nobody until 08/07, so the
	// promise was never checked: `trait B: A` parsed, `impl B for S` compiled with no
	// `A` in sight, and the declaration said something the compiler did not enforce.
	// Found by sweeping the AST for fields nothing reads.
	CodeUnsatisfiedSupertrait = "lyra-E040"

	// CodeNominalNewtypeBase: a `newtype` whose base is a type that already has nominal
	// identity — a `struct`, a `data` type, or a *named* tuple.
	//
	// `newtype` exists to give nominal identity to a **structural** type: `newtype
	// Meters = f64` makes an f64 that is not interchangeable with other f64s, and
	// `newtype Rgb = (u8, u8, u8)` does the same for an anonymous tuple. A struct or a
	// data type is already its own type, so wrapping one buys a second name and nothing
	// else — and the three nominal declarations are already distinguished on purpose
	// (todo.md's consistency section), so a fourth way to get a nominal product is the
	// redundancy that section exists to avoid.
	//
	// The evidence agreed before the rule did: a struct-based newtype could not be
	// constructed by any spelling, and a data-based one type-checked and crashed the
	// backend. Neither had ever been usable.
	CodeNominalNewtypeBase = "lyra-E041"

	// CodeOperatorNotOverloaded: an overloadable operator applied to an operand whose
	// type is a **type parameter**, so the impl it would dispatch to is not known
	// where the operator is written.
	//
	// `==` has no such problem — equality is structural, so a type variable is
	// comparable and the specialization only *overrides* that. Arithmetic has no
	// structural fallback: there is nothing `a + b` can mean for an unknown `t`. The
	// message says so directly rather than letting the operand reach the built-in
	// numeric rule, which would report "operands must be numeric" — true of a type
	// parameter, and no help at all.
	CodeOperatorNotOverloaded = "lyra-E042"

	// CodeNewtypeArithmeticOptIn: an integer overflow-arithmetic builtin
	// (`wrapping_*`/`saturating_*`/`checked_*`) called on a `newtype` receiver.
	//
	// Arithmetic on a newtype is opt-in: `Cents + Cents` is refused until the type
	// has an operator impl, because a nominal type's arithmetic is its own to define.
	// The overflow-arithmetic methods are those operators' escape hatches, so letting
	// them reach the base through method transparency handed out exactly the
	// arithmetic the operator rule withholds — and worse, since the base-typed
	// parameter accepted a *mixed* operand (`cents.wrapping_add(plain_i64)`), the one
	// silent unit-mixup a newtype exists to prevent. The safe spelling refused while
	// the unchecked one was accepted — a pit of success inverted (found 08/12).
	//
	// Method transparency itself stays: it was argued for `len`/`slice`/`trim` on a
	// wrapped string and remains right there — none of those is an operator's escape
	// hatch. The refusal is exactly the overflow-arithmetic family, and the message
	// names both explicit paths through: an operator impl, or reading the value into
	// its base (`let raw: i64 = c` — one-step read-out is documented assignability,
	// which is also why "require the argument to match the newtype" was not the fix:
	// base → newtype is assignable *by construction*, so a Cents parameter accepts a
	// plain i64 everywhere in the language, and enforcing strictness only here would
	// disagree with every other parameter).
	CodeNewtypeArithmeticOptIn = "lyra-E043"

	// CodeNewtypeConstructorCall: a malformed `newtype` construction — `Cents(150)`, or
	// its juxtaposed twin `Cents 150`, written with the wrong operand count, with an
	// operand the base cannot hold, or on a *generic* newtype (whose base is a type
	// variable, so there is nothing to check against until the parameters are bound).
	//
	// A newtype takes exactly one operand because it names exactly one base, and that
	// operand is checked against the **base** rather than against the newtype:
	// construction is precisely the act of turning a base value into a newtype value,
	// so requiring the operand to already be one would make the constructor useless.
	//
	// This code first existed, for about an hour on 08/12, to say a newtype had *no*
	// constructor — which was true then, and before that the same program reported
	// "Cents: not a tuple type" (a newtype construction parses as a `tuple_literal`),
	// naming the parse rather than the language. Constructors landed the same day, so
	// the code now covers what is still malformed rather than the form itself.
	CodeNewtypeConstructorCall = "lyra-E044"

	// CodeInertDerive: a `@derive(X)` naming a trait the compiler does not synthesize,
	// so the attribute does nothing. A warning rather than an error — the derive is not
	// wrong, the trait simply does not exist yet — but reported, because an attribute
	// that compiles and silently does nothing is the phantom-builtin shape this
	// compiler keeps having to dig out. Becomes moot when the trait lands.
	CodeInertDerive = "lyra-W014"

	// CodeInertOperatorMethod: an operator-named trait method (`(_+_)`, `(-_)`) that
	// nothing dispatches to. The grammar reserves twenty of these and every consumer
	// filters on an *identifier* method name, so the declaration parses, collects, and
	// is skipped — an impl of one is never called. A warning rather than an error
	// because user-defined arithmetic operators are the standing design for `(_+_)` on
	// a vector type; the comparison operators are a different case and are refused
	// outright (lyra-E039).
	CodeInertOperatorMethod = "lyra-W015"

	// CodeImportShadowed: a declaration takes a name a module this one imports
	// exports. The declaration wins and the imported one stays reachable through its
	// namespace (`seq.map`).
	//
	// This was a hard error until 08/08 — `import util.seq` plus an ordinary
	// `let map = …` simply would not compile — which read as "the module you imported
	// owns that name and your program may not have one". The comparison is what made
	// it wrong rather than merely strict: the *prelude*, whose names you never asked
	// for, took the soft path (W012) and let the user's declaration win, so the
	// explicit act was punished and the implicit one forgiven. One rule now, and this
	// is W012's sibling for the half that can name a qualifier to reach past itself.
	CodeImportShadowed = "lyra-W016"

	CodeShadowing       = "lyra-W001"
	CodeUnreachableCode = "lyra-W002"
	CodeUnusedVariable  = "lyra-W003"
	CodeUnusedImport    = "lyra-W004"
	CodeUnusedParameter = "lyra-W005"
	CodeUnusedResult    = "lyra-W006"

	// CodeNonOptionalCoalescing: the left operand of `??` is not a Maybe<T>, so
	// it can never be null and the coalescing is pointless.
	CodeNonOptionalCoalescing = "lyra-W007"

	// CodeImpreciseFloatEquality: an exact floating-point equality test — the
	// `==`/`!=` operator on floats, or a float literal `match` pattern (which
	// lowers to `fcmp oeq`). A value off by an ULP silently compares unequal, so
	// results may be surprising; a tolerance check or a range pattern is safer.
	CodeImpreciseFloatEquality = "lyra-W008"

	// CodeScreamingCaseTypeName: a type declared with an all-uppercase
	// (SCREAMING_CASE) name. Such a name lexes as a `const_identifier`, not a
	// `user_defined_type_name`, so a struct literal `NAME { … }` won't parse — the
	// type can be referenced but never constructed. Give it a PascalCase name.
	CodeScreamingCaseTypeName = "lyra-W009"

	// CodeInertBorrowModifier: an `own`/`ref`/`mut` modifier on a parameter whose
	// type is a copied scalar primitive (a numeric type, `bool`, or `rune`). These
	// modifiers are calling conventions over a *reference* — `own` transfers
	// ownership, `ref`/`mut` borrow — but a scalar is passed by value with no
	// interior to borrow or transfer, so the modifier is equivalent to a plain
	// parameter and only misleads a reader into expecting move/borrow semantics.
	// `string` (a managed fat pointer) and generic type parameters are NOT scalars
	// and are never flagged. A warning, since the code is correct as written.
	CodeInertBorrowModifier = "lyra-W010"

	// CodeConstantComparison: an integer comparison whose operand ranges (tracked by
	// the value-range analysis) prove it always evaluates to the same result — e.g.
	// `x < 0` on a `u8` (always false), or a comparison made trivial by a branch
	// refinement. The branch it guards is dead code or a likely bug; a warning since
	// the code still compiles and runs.
	CodeConstantComparison = "lyra-W011"

	// CodePreludeShadowed: a declaration takes a name the prelude exports. The
	// declaration wins. A warning rather than an error because the prelude is
	// implicitly in scope everywhere: rejecting the clash would make every name it
	// exports permanently unusable, and adding a name to the prelude later would
	// break programs that never mentioned it.
	CodePreludeShadowed = "lyra-W012"

	// CodeUnusedTypeParameter: a generic parameter declared in a written list that
	// the signature never mentions — `let f<t, u> = (a: t) -> t => a`.
	//
	// The sibling of E031, and the other half of reconciling a written list with
	// its signature. A warning rather than an error because the code is correct as
	// written: an unused variable is solved by nothing, constrains nothing, and
	// changes no call site. What makes it worth reporting is that the list is the
	// only place a *bound* can be written, so `<u: Show>` on a variable the
	// signature never mentions is a constraint that silently constrains nothing —
	// the reading a programmer is least likely to expect.
	CodeUnusedTypeParameter = "lyra-W013"
)
