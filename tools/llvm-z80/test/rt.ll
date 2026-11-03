; The helpers the Z80 target calls, for the tests: what the runtime will provide. Written with
; nothing that would itself become a call.

define i16 @__mulhi3(i16 %a, i16 %b) {
entry:
  br label %loop
loop:
  %x = phi i16 [ %a, %entry ], [ %x2, %next ]
  %y = phi i16 [ %b, %entry ], [ %y2, %next ]
  %r = phi i16 [ 0, %entry ], [ %r2, %next ]
  %done = icmp eq i16 %y, 0
  br i1 %done, label %out, label %body
body:
  %bit = and i16 %y, 1
  %odd = icmp ne i16 %bit, 0
  %sum = add i16 %r, %x
  %r2 = select i1 %odd, i16 %sum, i16 %r
  br label %next
next:
  %x2 = shl i16 %x, 1
  %y2 = lshr i16 %y, 1
  br label %loop
out:
  ret i16 %r
}

define i8 @__mulqi3(i8 %a, i8 %b) {
  %wa = zext i8 %a to i16
  %wb = zext i8 %b to i16
  %r = call i16 @__mulhi3(i16 %wa, i16 %wb)
  %t = trunc i16 %r to i8
  ret i8 %t
}

; Quotient by shift and subtract, remainder through the pointer.
define i16 @__z80_udivmod(i16 %n, i16 %d, ptr %rem) {
entry:
  br label %loop
loop:
  %i = phi i8 [ 16, %entry ], [ %i2, %cont ]
  %q = phi i16 [ %n, %entry ], [ %q3, %cont ]
  %r = phi i16 [ 0, %entry ], [ %r3, %cont ]
  %top = lshr i16 %q, 15
  %r1 = shl i16 %r, 1
  %r2 = or i16 %r1, %top
  %q2 = shl i16 %q, 1
  %fits = icmp uge i16 %r2, %d
  br i1 %fits, label %take, label %cont
take:
  %rs = sub i16 %r2, %d
  %qs = or i16 %q2, 1
  br label %cont
cont:
  %r3 = phi i16 [ %r2, %loop ], [ %rs, %take ]
  %q3 = phi i16 [ %q2, %loop ], [ %qs, %take ]
  %i2 = sub i8 %i, 1
  %more = icmp ne i8 %i2, 0
  br i1 %more, label %loop, label %out
out:
  store i16 %r3, ptr %rem
  ret i16 %q3
}

define i16 @__udivhi3(i16 %n, i16 %d) {
  %rem = alloca i16
  %q = call i16 @__z80_udivmod(i16 %n, i16 %d, ptr %rem)
  ret i16 %q
}

define i16 @__umodhi3(i16 %n, i16 %d) {
  %rem = alloca i16
  %q = call i16 @__z80_udivmod(i16 %n, i16 %d, ptr %rem)
  %r = load i16, ptr %rem
  ret i16 %r
}

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
