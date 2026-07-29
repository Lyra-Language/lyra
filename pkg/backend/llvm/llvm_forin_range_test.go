package llvm

import "testing"

// `for i in START..<END` (and `..=`, with an optional `:step`) lowers as a counter
// loop: the counter is the loop variable, `..<` is an exclusive end and `..=`
// inclusive.
func TestExec_ForInRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"exclusive range sums 0..3",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<4 {
    sum += u8(i)
  }
  sum
}`,
			6, // 0+1+2+3
		},
		{
			"inclusive range sums 0..=4",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..=4 {
    sum += u8(i)
  }
  sum
}`,
			10, // 0+1+2+3+4
		},
		{
			"variable end",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let n: i64 = 5
  for i in 0..<n {
    sum += u8(i)
  }
  sum
}`,
			10, // 0+1+2+3+4
		},
		{
			// Typed bounds make the counter that width (u8 here), no conversion needed.
			"typed u8 bounds",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let lo: u8 = 1
  let hi: u8 = 4
  for i in lo..<hi {
    sum += i
  }
  sum
}`,
			6, // 1+2+3
		},
		{
			"stepped range",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<10:2 {
    sum += u8(i)
  }
  sum
}`,
			20, // 0+2+4+6+8
		},
		{
			"break out of a range loop",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<10 {
    if i == 3 { break }
    sum += u8(i)
  }
  sum
}`,
			3, // 0+1+2
		},
		{
			"continue skips a value",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<5 {
    if i == 2 { continue }
    sum += u8(i)
  }
  sum
}`,
			8, // 0+1+3+4
		},
		{
			// The idiom: index a dynamic array by a range up to its length.
			"range over 0..<xs.len() indexes the array",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: []u8 = [10, 20, 12]
  for i in 0..<xs.len() {
    sum += xs[i]
  }
  sum
}`,
			42, // 10+20+12
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}
