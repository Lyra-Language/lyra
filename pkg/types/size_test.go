package types

import "testing"

func TestParseSizeIsCanonical(t *testing.T) {
	cases := []struct{ in, want string }{
		{"N", "N"},
		{"N + 1", "N+1"},
		{"1 + N", "N+1"},
		{"N - 1", "N-1"},
		{"2 * N + 1", "2*N+1"},
		{"N * 2 + 1", "2*N+1"},
		{"(N + 1) * 2", "2*N+2"},
		{"N + M", "M+N"},
		{"M + N - N", "M"},
		{"3 * 4", "12"},
		{"N - N", "0"},
		{"2*N+M+1", "M+2*N+1"},
		{"-N+3", "-N+3"},
	}
	for _, c := range cases {
		got, ok := ParseSize(c.in)
		if !ok || got != c.want {
			t.Errorf("ParseSize(%q) = %q, %v; want %q", c.in, got, ok, c.want)
		}
		if again, ok := ParseSize(got); !ok || again != got {
			t.Errorf("ParseSize(%q) isn't stable: %q", got, again)
		}
	}
	for _, in := range []string{"N * M", "(N + 1) * N", "N +", "", "N / 2", "N)"} {
		if got, ok := ParseSize(in); ok {
			t.Errorf("ParseSize(%q) = %q; want refused", in, got)
		}
	}
}

func TestSubstituteSize(t *testing.T) {
	bind := map[string]Type{"N": ArraySize{Size: 3}, "K": ArraySize{Var: "M+1"}}
	cases := []struct {
		in   string
		size int
		v    string
	}{
		{"N", 3, ""},
		{"N+1", 4, ""},
		{"2*N-7", -1, ""},
		{"K", 0, "M+1"},
		{"2*K+N", 0, "2*M+5"},
		{"Q+N", 0, "Q+3"},
	}
	for _, c := range cases {
		size, v := SubstituteSize(c.in, bind)
		if size != c.size || v != c.v {
			t.Errorf("SubstituteSize(%q) = %d, %q; want %d, %q", c.in, size, v, c.size, c.v)
		}
	}
}

func TestSolveSize(t *testing.T) {
	generics := map[string]bool{"N": true, "M": true}
	cases := []struct {
		name    string
		pattern string
		actual  ArraySize
		prior   map[string]Type
		ok      bool
		want    map[string]Type
	}{
		{"a name", "N", ArraySize{Size: 5}, nil, true, map[string]Type{"N": ArraySize{Size: 5}}},
		{"an offset", "N+1", ArraySize{Size: 5}, nil, true, map[string]Type{"N": ArraySize{Size: 4}}},
		{"a multiple", "2*N", ArraySize{Size: 6}, nil, true, map[string]Type{"N": ArraySize{Size: 3}}},
		{"no multiple", "2*N", ArraySize{Size: 5}, nil, false, nil},
		{"below zero", "N+3", ArraySize{Size: 2}, nil, false, nil},
		{"the caller's own name", "N", ArraySize{Var: "M"}, nil, true, map[string]Type{"N": ArraySize{Var: "M"}}},
		{"the caller's own name, offset", "N+1", ArraySize{Var: "Q"}, nil, true, map[string]Type{"N": ArraySize{Var: "Q-1"}}},
		{"bound and agrees", "N+1", ArraySize{Size: 4}, map[string]Type{"N": ArraySize{Size: 3}}, true, nil},
		{"bound and doesn't", "N+1", ArraySize{Size: 5}, map[string]Type{"N": ArraySize{Size: 3}}, false, nil},
		{"one of two bound", "N+M", ArraySize{Size: 5}, map[string]Type{"N": ArraySize{Size: 3}}, true, map[string]Type{"M": ArraySize{Size: 2}}},
		{"two unknown waits", "N+M", ArraySize{Size: 5}, nil, true, map[string]Type{}},
		{"not the callee's", "Q+1", ArraySize{Var: "Q+1"}, nil, true, map[string]Type{}},
		{"not the callee's, differs", "Q+1", ArraySize{Var: "Q"}, nil, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bindings := map[string]Type{}
			for k, v := range c.prior {
				bindings[k] = v
			}
			if got := SolveSize(c.pattern, c.actual, generics, bindings); got != c.ok {
				t.Fatalf("SolveSize = %v; want %v", got, c.ok)
			}
			for k, want := range c.want {
				if bindings[k] != want {
					t.Errorf("%s = %v; want %v", k, bindings[k], want)
				}
			}
		})
	}
}
