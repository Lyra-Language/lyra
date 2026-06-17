package regex

import (
	"sync"
	"testing"
)

// TestConcurrentMatching_NoRace exercises IsMatch/FindAll from many goroutines
// on a shared *Regex whose DFA is built lazily (a Unicode-property + intersection
// pattern is too complex to be frozen at compile time, so states are interned
// during matching). Run with -race to catch regressions in the DFA / lookahead
// cache synchronization.
func TestConcurrentMatching_NoRace(t *testing.T) {
	patterns := []string{
		`\p{L}+\p{N}*&_*[A-Z]_*`, // lazy main DFA (intersection + unicode props)
		`foo(?=bar)`,             // trailing lookahead → laCache
		`(?<=ab)cd`,              // leading lookbehind gate
	}
	inputs := [][]byte{
		[]byte("Hello123"),
		[]byte("Wörld42X"),
		[]byte("foobar"),
		[]byte("abcd"),
		[]byte("ZZZ999zzz"),
		[]byte(""),
	}
	for _, p := range patterns {
		re := MustCompile(p)
		var wg sync.WaitGroup
		for g := 0; g < 16; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 300; i++ {
					in := inputs[i%len(inputs)]
					_, _ = re.IsMatch(in)
					_, _ = re.FindAllIndex(in)
				}
			}()
		}
		wg.Wait()
	}
}
