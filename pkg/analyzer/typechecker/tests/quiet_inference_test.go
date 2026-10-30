package typechecker_test

import "testing"

// **An expression inferred quietly and inferred again reports what is wrong inside it.**
// Three places infer an expression once with its errors thrown away — an array literal's
// element set aside for want of context, a generic struct literal's field before its type
// is settled, a destructuring's places before the value — on the promise that the second
// inference reports them. The first cached a type for every expression it reached, so the
// second, finding them, inferred nothing, and an error inside a call that still had a type
// was never reported (10/30, in Vega: `row([text(…), select(SelectSpec { options: [i in
// offered | all[i]] })])` indexed with a bool and checked clean). TypeTable.Journal is how
// the first attempt's types are forgotten with its errors.
func TestQuietInference_ReportsWhatTheSecondInferenceFinds(t *testing.T) {
	const decls = `
struct Box<e> { v: i64 }
struct Holder<e> { item: e, n: i64 }
let lbl<e> = pure (s: string) -> Box<e> => Box { v: 0 }
let rowof<e> = pure (xs: []Box<e>) -> Box<e> => Box { v: 0 }
let boxed = pure (xs: []string) -> Box<i64> => Box { v: 0 }
let counted = pure (xs: []string) -> i64 => 0
let pair = pure () -> (i64, i64) => (1, 2)
`
	for _, c := range []struct{ name, src string }{
		{
			"an array literal's later element, passed to a generic function",
			`let f = pure (all: []string, flags: []bool) -> Box<i64> => rowof([lbl("p"), boxed([i in flags | all[i]])])`,
		},
		{
			"a generic struct literal's field inferred before its type is settled",
			`let f = pure (all: []string, flags: []bool) -> Holder<i64> => Holder { item: counted([i in flags | all[i]]), n: 1 }`,
		},
		{
			"a destructuring's place",
			`let f = (all: []string, flags: []bool) -> void => {
  var xs = [1, 2]
  var b = 0
  (xs[counted([i in flags | all[i]])], b) = pair()
}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, decls+c.src, false)
			assertErrorsAre(t, res, "index must be an integer, got bool")
		})
	}
}
