; The Z80 runtime: what a Lyra program compiled for the Master System or Game Gear needs beneath
; it, and the helpers the Z80 target calls for what it has no instructions for. The start-up,
; the interrupt and `__z80_callbc` are written by the linker (pkg/rom/z80.go).
;
; LLVM IR rather than assembly: it goes through `llc` alone (cmd/lyrac/z80.go), so a loop here is
; never turned back into a call to the function it implements. Nothing here may multiply,
; divide or shift by a variable, except by calling what is defined for it here.
;
; MIT, as Lyra is: it is linked into every game.

; `__mulhi3` and `__z80_umul32` are the linker's, hand-written (pkg/rom/z80.go): HL × DE.
declare i16 @__mulhi3(i16, i16)
declare i32 @__z80_umul32(i16, i16)

define i8 @__mulqi3(i8 %a, i8 %b) {
  %wa = zext i8 %a to i16
  %wb = zext i8 %b to i16
  %r = call i16 @__mulhi3(i16 %wa, i16 %wb)
  %t = trunc i16 %r to i8
  ret i8 %t
}

; `__udivhi3` and `__umodhi3` are the linker's too.
declare i16 @__udivhi3(i16, i16)
declare i16 @__umodhi3(i16, i16)

define i16 @__divhi3(i16 %n, i16 %d) {
  %nneg = icmp slt i16 %n, 0
  %dneg = icmp slt i16 %d, 0
  %nn = sub i16 0, %n
  %dd = sub i16 0, %d
  %un = select i1 %nneg, i16 %nn, i16 %n
  %ud = select i1 %dneg, i16 %dd, i16 %d
  %q = call i16 @__udivhi3(i16 %un, i16 %ud)
  %flip = xor i1 %nneg, %dneg
  %mq = sub i16 0, %q
  %r = select i1 %flip, i16 %mq, i16 %q
  ret i16 %r
}

define i16 @__modhi3(i16 %n, i16 %d) {
  %nneg = icmp slt i16 %n, 0
  %dneg = icmp slt i16 %d, 0
  %nn = sub i16 0, %n
  %dd = sub i16 0, %d
  %un = select i1 %nneg, i16 %nn, i16 %n
  %ud = select i1 %dneg, i16 %dd, i16 %d
  %m = call i16 @__umodhi3(i16 %un, i16 %ud)
  %mm = sub i16 0, %m
  %r = select i1 %nneg, i16 %mm, i16 %m
  ret i16 %r
}

define i16 @__ashlhi3(i16 %x, i8 %n) {
entry:
  br label %loop
loop:
  %v = phi i16 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = shl i16 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i16 %v
}

define i16 @__lshrhi3(i16 %x, i8 %n) {
entry:
  br label %loop
loop:
  %v = phi i16 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = lshr i16 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i16 %v
}

define i16 @__ashrhi3(i16 %x, i8 %n) {
entry:
  br label %loop
loop:
  %v = phi i16 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = ashr i16 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i16 %v
}

define i8 @__udivqi3(i8 %n, i8 %d) {
  %wn = zext i8 %n to i16
  %wd = zext i8 %d to i16
  %r = call i16 @__udivhi3(i16 %wn, i16 %wd)
  %t = trunc i16 %r to i8
  ret i8 %t
}

define i8 @__umodqi3(i8 %n, i8 %d) {
  %wn = zext i8 %n to i16
  %wd = zext i8 %d to i16
  %r = call i16 @__umodhi3(i16 %wn, i16 %wd)
  %t = trunc i16 %r to i8
  ret i8 %t
}

define i8 @__divqi3(i8 %n, i8 %d) {
  %wn = sext i8 %n to i16
  %wd = sext i8 %d to i16
  %r = call i16 @__divhi3(i16 %wn, i16 %wd)
  %t = trunc i16 %r to i8
  ret i8 %t
}

define i8 @__modqi3(i8 %n, i8 %d) {
  %wn = sext i8 %n to i16
  %wd = sext i8 %d to i16
  %r = call i16 @__modhi3(i16 %wn, i16 %wd)
  %t = trunc i16 %r to i8
  ret i8 %t
}

; ── Wider arithmetic ──────────────────────────────────────────────────────────────────

; a × b from the 16-bit multiply: low × low, and the cross terms' low halves shifted up. The
; high × high term is entirely past 32 bits.
define i32 @__mulsi3(i32 %a, i32 %b) {
entry:
  %al = trunc i32 %a to i16
  %bl = trunc i32 %b to i16
  %low = call i32 @__z80_umul32(i16 %al, i16 %bl)
  %ah32 = lshr i32 %a, 16
  %bh32 = lshr i32 %b, 16
  %ah = trunc i32 %ah32 to i16
  %bh = trunc i32 %bh32 to i16
  %small = or i16 %ah, %bh
  %simple = icmp eq i16 %small, 0
  br i1 %simple, label %out, label %cross
cross:
  %x = call i16 @__mulhi3(i16 %ah, i16 %bl)
  %y = call i16 @__mulhi3(i16 %al, i16 %bh)
  %sum = add i16 %x, %y
  %wide = zext i16 %sum to i32
  %up = shl i32 %wide, 16
  %r = add i32 %low, %up
  br label %out
out:
  %answer = phi i32 [ %low, %entry ], [ %r, %cross ]
  ret i32 %answer
}

define i64 @__muldi3(i64 %a, i64 %b) {
entry:
  br label %loop
loop:
  %x = phi i64 [ %a, %entry ], [ %x2, %body ]
  %y = phi i64 [ %b, %entry ], [ %y2, %body ]
  %r = phi i64 [ 0, %entry ], [ %r2, %body ]
  %done = icmp eq i64 %y, 0
  br i1 %done, label %out, label %body
body:
  %bit = and i64 %y, 1
  %odd = icmp ne i64 %bit, 0
  %sum = add i64 %r, %x
  %r2 = select i1 %odd, i64 %sum, i64 %r
  %x2 = shl i64 %x, 1
  %y2 = lshr i64 %y, 1
  br label %loop
out:
  ret i64 %r
}

define i32 @__z80_udivmodsi(i32 %n, i32 %d, ptr %rem) {
entry:
  br label %loop
loop:
  %i = phi i8 [ 32, %entry ], [ %i2, %cont ]
  %q = phi i32 [ %n, %entry ], [ %q3, %cont ]
  %r = phi i32 [ 0, %entry ], [ %r3, %cont ]
  %top = lshr i32 %q, 31
  %r1 = shl i32 %r, 1
  %r2 = or i32 %r1, %top
  %q2 = shl i32 %q, 1
  %fits = icmp uge i32 %r2, %d
  br i1 %fits, label %take, label %cont
take:
  %rs = sub i32 %r2, %d
  %qs = or i32 %q2, 1
  br label %cont
cont:
  %r3 = phi i32 [ %r2, %loop ], [ %rs, %take ]
  %q3 = phi i32 [ %q2, %loop ], [ %qs, %take ]
  %i2 = sub i8 %i, 1
  %more = icmp ne i8 %i2, 0
  br i1 %more, label %loop, label %out
out:
  store i32 %r3, ptr %rem
  ret i32 %q3
}

define i32 @__udivsi3(i32 %n, i32 %d) {
  %rem = alloca i32
  %q = call i32 @__z80_udivmodsi(i32 %n, i32 %d, ptr %rem)
  ret i32 %q
}

define i32 @__umodsi3(i32 %n, i32 %d) {
  %rem = alloca i32
  %q = call i32 @__z80_udivmodsi(i32 %n, i32 %d, ptr %rem)
  %r = load i32, ptr %rem
  ret i32 %r
}

define i32 @__divsi3(i32 %n, i32 %d) {
  %nneg = icmp slt i32 %n, 0
  %dneg = icmp slt i32 %d, 0
  %nn = sub i32 0, %n
  %dd = sub i32 0, %d
  %un = select i1 %nneg, i32 %nn, i32 %n
  %ud = select i1 %dneg, i32 %dd, i32 %d
  %q = call i32 @__udivsi3(i32 %un, i32 %ud)
  %flip = xor i1 %nneg, %dneg
  %mq = sub i32 0, %q
  %r = select i1 %flip, i32 %mq, i32 %q
  ret i32 %r
}

define i32 @__modsi3(i32 %n, i32 %d) {
  %nneg = icmp slt i32 %n, 0
  %dneg = icmp slt i32 %d, 0
  %nn = sub i32 0, %n
  %dd = sub i32 0, %d
  %un = select i1 %nneg, i32 %nn, i32 %n
  %ud = select i1 %dneg, i32 %dd, i32 %d
  %m = call i32 @__umodsi3(i32 %un, i32 %ud)
  %mm = sub i32 0, %m
  %r = select i1 %nneg, i32 %mm, i32 %m
  ret i32 %r
}

define i64 @__z80_udivmoddi(i64 %n, i64 %d, ptr %rem) {
entry:
  br label %loop
loop:
  %i = phi i8 [ 64, %entry ], [ %i2, %cont ]
  %q = phi i64 [ %n, %entry ], [ %q3, %cont ]
  %r = phi i64 [ 0, %entry ], [ %r3, %cont ]
  %top = lshr i64 %q, 63
  %r1 = shl i64 %r, 1
  %r2 = or i64 %r1, %top
  %q2 = shl i64 %q, 1
  %fits = icmp uge i64 %r2, %d
  br i1 %fits, label %take, label %cont
take:
  %rs = sub i64 %r2, %d
  %qs = or i64 %q2, 1
  br label %cont
cont:
  %r3 = phi i64 [ %r2, %loop ], [ %rs, %take ]
  %q3 = phi i64 [ %q2, %loop ], [ %qs, %take ]
  %i2 = sub i8 %i, 1
  %more = icmp ne i8 %i2, 0
  br i1 %more, label %loop, label %out
out:
  store i64 %r3, ptr %rem
  ret i64 %q3
}

define i64 @__udivdi3(i64 %n, i64 %d) {
  %rem = alloca i64
  %q = call i64 @__z80_udivmoddi(i64 %n, i64 %d, ptr %rem)
  ret i64 %q
}

define i64 @__umoddi3(i64 %n, i64 %d) {
  %rem = alloca i64
  %q = call i64 @__z80_udivmoddi(i64 %n, i64 %d, ptr %rem)
  %r = load i64, ptr %rem
  ret i64 %r
}

define i64 @__divdi3(i64 %n, i64 %d) {
  %nneg = icmp slt i64 %n, 0
  %dneg = icmp slt i64 %d, 0
  %nn = sub i64 0, %n
  %dd = sub i64 0, %d
  %un = select i1 %nneg, i64 %nn, i64 %n
  %ud = select i1 %dneg, i64 %dd, i64 %d
  %q = call i64 @__udivdi3(i64 %un, i64 %ud)
  %flip = xor i1 %nneg, %dneg
  %mq = sub i64 0, %q
  %r = select i1 %flip, i64 %mq, i64 %q
  ret i64 %r
}

define i64 @__moddi3(i64 %n, i64 %d) {
  %nneg = icmp slt i64 %n, 0
  %dneg = icmp slt i64 %d, 0
  %nn = sub i64 0, %n
  %dd = sub i64 0, %d
  %un = select i1 %nneg, i64 %nn, i64 %n
  %ud = select i1 %dneg, i64 %dd, i64 %d
  %m = call i64 @__umoddi3(i64 %un, i64 %ud)
  %mm = sub i64 0, %m
  %r = select i1 %nneg, i64 %mm, i64 %m
  ret i64 %r
}

define i32 @__ashlsi3(i32 %x, i32 %n) {
entry:
  %n8 = trunc i32 %n to i8
  br label %loop
loop:
  %v = phi i32 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n8, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = shl i32 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i32 %v
}

define i64 @__ashldi3(i64 %x, i32 %n) {
entry:
  %n8 = trunc i32 %n to i8
  br label %loop
loop:
  %v = phi i64 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n8, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = shl i64 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i64 %v
}

define i32 @__lshrsi3(i32 %x, i32 %n) {
entry:
  %n8 = trunc i32 %n to i8
  br label %loop
loop:
  %v = phi i32 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n8, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = lshr i32 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i32 %v
}

define i64 @__lshrdi3(i64 %x, i32 %n) {
entry:
  %n8 = trunc i32 %n to i8
  br label %loop
loop:
  %v = phi i64 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n8, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = lshr i64 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i64 %v
}

define i32 @__ashrsi3(i32 %x, i32 %n) {
entry:
  %n8 = trunc i32 %n to i8
  br label %loop
loop:
  %v = phi i32 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n8, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = ashr i32 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i32 %v
}

define i64 @__ashrdi3(i64 %x, i32 %n) {
entry:
  %n8 = trunc i32 %n to i8
  br label %loop
loop:
  %v = phi i64 [ %x, %entry ], [ %v2, %body ]
  %i = phi i8 [ %n8, %entry ], [ %i2, %body ]
  %done = icmp eq i8 %i, 0
  br i1 %done, label %out, label %body
body:
  %v2 = ashr i64 %v, 1
  %i2 = sub i8 %i, 1
  br label %loop
out:
  ret i64 %v
}

; A multiply checked for overflow: the answer, and through the pointer whether it was wrong. The
; product of two i32s fits an i64, and an i64's is checked by dividing back.
;
; **The flag is pointer-sized, an i16**, whatever C's `int` is: LLVM's call makes a slot that big.
; An i32 written there overwrote the two bytes beside it, a saved IX for one, and a game crashed
; a call later; a test checks the neighbour now.
define i32 @__mulosi4(i32 %a, i32 %b, ptr %overflow) {
entry:
  store i16 0, ptr %overflow
  ; Both fitting 16 bits, as nearly all do, the product cannot overflow: sizes and signs apart.
  %at = trunc i32 %a to i16
  %bt = trunc i32 %b to i16
  %ax = sext i16 %at to i32
  %bx = sext i16 %bt to i32
  %af = icmp eq i32 %ax, %a
  %bf = icmp eq i32 %bx, %b
  %fit = and i1 %af, %bf
  br i1 %fit, label %small, label %large
small:
  %an = icmp slt i16 %at, 0
  %bn = icmp slt i16 %bt, 0
  %ana = sub i16 0, %at
  %bna = sub i16 0, %bt
  %am = select i1 %an, i16 %ana, i16 %at
  %bm = select i1 %bn, i16 %bna, i16 %bt
  %mag = call i32 @__z80_umul32(i16 %am, i16 %bm)
  %flip = xor i1 %an, %bn
  %neg = sub i32 0, %mag
  %r = select i1 %flip, i32 %neg, i32 %mag
  ret i32 %r
large:
  %wa = sext i32 %a to i64
  %wb = sext i32 %b to i64
  %wide = call i64 @__muldi3(i64 %wa, i64 %wb)
  %lr = trunc i64 %wide to i32
  %back = sext i32 %lr to i64
  %wrong = icmp ne i64 %back, %wide
  %flag = zext i1 %wrong to i16
  store i16 %flag, ptr %overflow
  ret i32 %lr
}

define i64 @__mulodi4(i64 %a, i64 %b, ptr %overflow) {
entry:
  %r = call i64 @__muldi3(i64 %a, i64 %b)
  store i16 0, ptr %overflow
  %zero = icmp eq i64 %a, 0
  br i1 %zero, label %out, label %check
check:
  ; The one quotient a divide cannot do, MIN / -1, is its own overflow.
  %minus = icmp eq i64 %a, -1
  %least = icmp eq i64 %b, -9223372036854775808
  %both = and i1 %minus, %least
  br i1 %both, label %over, label %divide
divide:
  %q = call i64 @__divdi3(i64 %r, i64 %a)
  %same = icmp eq i64 %q, %b
  br i1 %same, label %out, label %over
over:
  store i16 1, ptr %overflow
  br label %out
out:
  ret i64 %r
}

; ── Memory ─────────────────────────────────────────────────────────────────────────────

define ptr @memcpy(ptr %to, ptr %from, i16 %n) {
entry:
  br label %loop
loop:
  %i = phi i16 [ 0, %entry ], [ %i2, %body ]
  %done = icmp eq i16 %i, %n
  br i1 %done, label %out, label %body
body:
  %s = getelementptr i8, ptr %from, i16 %i
  %d = getelementptr i8, ptr %to, i16 %i
  %b = load i8, ptr %s
  store i8 %b, ptr %d
  %i2 = add i16 %i, 1
  br label %loop
out:
  ret ptr %to
}

define ptr @memmove(ptr %to, ptr %from, i16 %n) {
entry:
  %forward = icmp ule ptr %to, %from
  br i1 %forward, label %up, label %down
up:
  %r = call ptr @memcpy(ptr %to, ptr %from, i16 %n)
  ret ptr %to
down:
  %i = phi i16 [ %n, %entry ], [ %i2, %body ]
  %done = icmp eq i16 %i, 0
  br i1 %done, label %out, label %body
body:
  %i2 = sub i16 %i, 1
  %s = getelementptr i8, ptr %from, i16 %i2
  %d = getelementptr i8, ptr %to, i16 %i2
  %b = load i8, ptr %s
  store i8 %b, ptr %d
  br label %down
out:
  ret ptr %to
}

; LLVM's call passes the byte as an i8, not C's int: an i32 here took the length off the stack
; a slot too far, and cleared whatever length that read as.
define ptr @memset(ptr %to, i8 %byte, i16 %n) {
entry:
  br label %loop
loop:
  %i = phi i16 [ 0, %entry ], [ %i2, %body ]
  %done = icmp eq i16 %i, %n
  br i1 %done, label %out, label %body
body:
  %d = getelementptr i8, ptr %to, i16 %i
  store i8 %byte, ptr %d
  %i2 = add i16 %i, 1
  br label %loop
out:
  ret ptr %to
}

define i32 @memcmp(ptr %a, ptr %b, i16 %n) {
entry:
  br label %loop
loop:
  %i = phi i16 [ 0, %entry ], [ %i2, %same ]
  %done = icmp eq i16 %i, %n
  br i1 %done, label %equal, label %body
body:
  %p = getelementptr i8, ptr %a, i16 %i
  %q = getelementptr i8, ptr %b, i16 %i
  %x = load i8, ptr %p
  %y = load i8, ptr %q
  %i2 = add i16 %i, 1
  %eq = icmp eq i8 %x, %y
  br i1 %eq, label %same, label %differ
same:
  br label %loop
differ:
  %less = icmp ult i8 %x, %y
  %r = select i1 %less, i32 -1, i32 1
  ret i32 %r
equal:
  ret i32 0
}

; ── A trap ─────────────────────────────────────────────────────────────────────────────

; The end of a program that panicked: the screen red, as on the Genesis, and no more. The display
; on (register 1), then colour 0 of the background's palette; an empty name table shows it.
define void @exit(i32 %code) noreturn {
entry:
  store volatile i8 192, ptr addrspace(1) inttoptr (i16 191 to ptr addrspace(1))
  store volatile i8 129, ptr addrspace(1) inttoptr (i16 191 to ptr addrspace(1))
  store volatile i8 0, ptr addrspace(1) inttoptr (i16 191 to ptr addrspace(1))
  store volatile i8 192, ptr addrspace(1) inttoptr (i16 191 to ptr addrspace(1))
  store volatile i8 3, ptr addrspace(1) inttoptr (i16 190 to ptr addrspace(1))
  br label %stop
stop:
  br label %stop
}
