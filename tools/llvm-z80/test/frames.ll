; Frames large enough that IX sits above SP (Z80FrameLowering), and slots on both sides of it.

@sink = global i16 0

define i16 @w(i16 %x) { ret i16 %x }
define ptr @keep(ptr %x) { ret ptr %x }

; Stack arguments: an outgoing argument counted from SP, from a frame with IX moved.
define i16 @five(i16 %a, i16 %b, i16 %c, i16 %d, i16 %e) {
  %s = add i16 %a, %b
  %t = add i16 %s, %c
  %u = add i16 %t, %d
  %v = add i16 %u, %e
  ret i16 %v }

; test big_frame_args hl=15
define i16 @big_frame_args() {
  %buf = alloca [100 x i16]
  %q = call ptr @keep(ptr %buf)
  %r = call i16 @five(i16 1, i16 2, i16 3, i16 4, i16 5)
  ret i16 %r }

; Incoming stack arguments read across a frame larger than a displacement reaches.
define i16 @reads_far(i16 %a, i16 %b, i16 %c, i16 %d, i16 %e) {
  %buf = alloca [200 x i16]
  %q = call ptr @keep(ptr %buf)
  %p = getelementptr i16, ptr %q, i16 150
  store i16 %e, ptr %p
  %x = load i16, ptr %p
  %s = add i16 %x, %c
  %t = add i16 %s, %d
  ret i16 %t }

; test incoming_past_reach hl=12
define i16 @incoming_past_reach() {
  %r = call i16 @reads_far(i16 1, i16 2, i16 3, i16 4, i16 5)
  ret i16 %r }

; Every slot of a large frame written and read, near SP, near the middle and near the top, where
; the address taken (LEA) has to agree with the slot's displacement.
; test slots_across_the_frame hl=1234
define i16 @slots_across_the_frame() {
  %low = alloca [130 x i8]
  %high = alloca [130 x i8]
  %q = call ptr @keep(ptr %low)
  %o = call ptr @keep(ptr %high)
  %a = getelementptr i8, ptr %low, i16 0
  %b = getelementptr i8, ptr %low, i16 129
  %c = getelementptr i8, ptr %high, i16 0
  %d = getelementptr i8, ptr %high, i16 129
  %a16 = getelementptr i8, ptr %q, i16 1
  %d16 = getelementptr i8, ptr %o, i16 120
  store i16 100, ptr %a16
  store i16 200, ptr %d16
  store i8 7, ptr %a
  store i8 8, ptr %b
  store i8 9, ptr %c
  store i8 10, ptr %d
  %x = load i16, ptr %a16
  %y = load i16, ptr %d16
  %k1 = load i8, ptr %a
  %k2 = load i8, ptr %b
  %k3 = load i8, ptr %c
  %k4 = load i8, ptr %d
  %m = zext i8 %k1 to i16
  %n = zext i8 %k2 to i16
  %u = zext i8 %k3 to i16
  %v = zext i8 %k4 to i16
  %s1 = add i16 %x, %y
  %s2 = add i16 %s1, %m
  %s3 = add i16 %s2, %n
  %s4 = add i16 %s3, %u
  %s5 = add i16 %s4, %v
  %s6 = sub i16 %s5, 34
  %s7 = add i16 %s6, 934
  ret i16 %s7 }

; The caller's IX comes back whatever the callee did with its own.
; test frame_then_frame hl=15
define i16 @frame_then_frame() {
  %buf = alloca i16
  store i16 3, ptr %buf
  %q = call ptr @keep(ptr %buf)
  %r = call i16 @reads_far(i16 1, i16 2, i16 3, i16 4, i16 5)
  %x = load i16, ptr %buf
  %s = add i16 %r, %x
  ret i16 %s }
