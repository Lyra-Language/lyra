; Spill slots at fixed addresses (Z80StaticSpills), and the functions that must not have them.
; Each function here holds more 16-bit values than the Z80 has registers for, across a call.

define i16 @w(i16 %x) { ret i16 %x }
@fp = global ptr @indirect_down
@table = global [4 x i16] [i16 3, i16 5, i16 7, i16 11]

; test many_values hl=1840
define i16 @many_values() {
  %r = call i16 @crowded(i16 3)
  ret i16 %r }

define internal i16 @crowded(i16 %n) {
  %a = call i16 @w(i16 %n)
  %b = call i16 @w(i16 5)
  %c = call i16 @w(i16 7)
  %d = call i16 @w(i16 11)
  %e = call i16 @w(i16 13)
  %f = call i16 @w(i16 17)
  %g = call i16 @w(i16 19)
  %x1 = add i16 %a, %b
  %x2 = xor i16 %c, %d
  %x3 = sub i16 %e, %f
  %y = call i16 @w(i16 %x1)
  %m = mul i16 %y, %g
  %s1 = add i16 %m, %x2
  %s2 = add i16 %s1, %x3
  %s3 = mul i16 %s2, %d
  %s4 = add i16 %s3, %a
  %s5 = add i16 %s4, %b
  %s6 = add i16 %s5, %c
  %s7 = add i16 %s6, %e
  %s8 = add i16 %s7, %f
  %s9 = add i16 %s8, %g
  %s10 = add i16 %s9, %x1
  %s11 = add i16 %s10, %x2
  %s12 = add i16 %s11, %x3
  ret i16 %s12 }

; Its own values live across its own call: a fixed slot would be overwritten by the call. Internal,
; as a game's are, so that only the check for a cycle stands in the way.
; test recursive hl=548
define i16 @recursive() {
  %r = call i16 @down(i16 6)
  ret i16 %r }

define internal i16 @down(i16 %n) {
  %c = icmp eq i16 %n, 0
  br i1 %c, label %end, label %more
end:
  ret i16 1
more:
  %a = call i16 @w(i16 %n)
  %b = add i16 %a, 3
  %d = mul i16 %a, %b
  %e = xor i16 %d, %a
  %f = add i16 %e, %b
  %m = sub i16 %n, 1
  %r = call i16 @down(i16 %m)
  %s1 = add i16 %r, %a
  %s2 = add i16 %s1, %b
  %s3 = add i16 %s2, %d
  %s4 = add i16 %s3, %e
  %s5 = add i16 %s4, %f
  ret i16 %s5 }

; The same through two functions.
; test mutually_recursive hl=320
define i16 @mutually_recursive() {
  %r = call i16 @even(i16 7)
  ret i16 %r }

define internal i16 @even(i16 %n) {
  %c = icmp eq i16 %n, 0
  br i1 %c, label %end, label %more
end:
  ret i16 1
more:
  %a = call i16 @w(i16 %n)
  %b = add i16 %a, 5
  %d = mul i16 %a, %b
  %m = sub i16 %n, 1
  %r = call i16 @odd(i16 %m)
  %s1 = add i16 %r, %a
  %s2 = add i16 %s1, %b
  %s3 = add i16 %s2, %d
  ret i16 %s3 }

define internal i16 @odd(i16 %n) {
  %c = icmp eq i16 %n, 0
  br i1 %c, label %end, label %more
end:
  ret i16 2
more:
  %a = call i16 @w(i16 %n)
  %b = xor i16 %a, 9
  %d = add i16 %a, %b
  %m = sub i16 %n, 1
  %r = call i16 @even(i16 %m)
  %s1 = add i16 %r, %a
  %s2 = add i16 %s1, %b
  %s3 = add i16 %s2, %d
  ret i16 %s3 }

; Recursion the graph can see only through a function's address.
; test recursive_through_a_pointer hl=92
define i16 @recursive_through_a_pointer() {
  %r = call i16 @indirect_down(i16 5)
  ret i16 %r }

define internal i16 @indirect_down(i16 %n) {
  %c = icmp eq i16 %n, 0
  br i1 %c, label %end, label %more
end:
  ret i16 1
more:
  %a = call i16 @w(i16 %n)
  %b = add i16 %a, 7
  %d = mul i16 %a, %b
  %e = xor i16 %d, %b
  %t = load ptr, ptr @fp
  %m = sub i16 %n, 1
  %r = call i16 %t(i16 %m)
  %s1 = add i16 %r, %a
  %s2 = add i16 %s1, %b
  %s3 = add i16 %s2, %d
  %s4 = sub i16 %s3, %e
  ret i16 %s4 }

; A slot read after a loop that writes it, in a function with fixed slots.
; test slots_round_a_loop hl=1113
define i16 @slots_round_a_loop() {
  %r = call i16 @loops(i16 4)
  ret i16 %r }

define internal i16 @loops(i16 %n) {
  %base = call i16 @w(i16 100)
  %step = call i16 @w(i16 3)
  br label %head
head:
  %i = phi i16 [ 0, %0 ], [ %i2, %head ]
  %s = phi i16 [ 0, %0 ], [ %s3, %head ]
  %t = phi i16 [ 1, %0 ], [ %t2, %head ]
  %el = getelementptr i16, ptr @table, i16 %i
  %v = load i16, ptr %el
  %u = call i16 @w(i16 %v)
  %s1 = mul i16 %u, %step
  %s2 = add i16 %s, %s1
  %s3 = add i16 %s2, %base
  %t2 = add i16 %t, %u
  %i2 = add i16 %i, 1
  %done = icmp eq i16 %i2, %n
  br i1 %done, label %out, label %head
out:
  %a = add i16 %s3, %t2
  %b = mul i16 %a, 2
  %c = add i16 %b, %base
  %d = add i16 %c, %step
  ret i16 %d }
