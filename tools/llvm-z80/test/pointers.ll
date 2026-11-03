; Fields through a pointer, which go through IY (Z80PointerBase). The pointers come through calls,
; so that each is a value only the running program knows. run.py requires `(iy` in this file's code.

@words = global [80 x i16] zeroinitializer
@bytes = global [8 x i8] [i8 1, i8 2, i8 3, i8 4, i8 5, i8 6, i8 7, i8 8]
@other = global [8 x i8] [i8 10, i8 20, i8 30, i8 40, i8 50, i8 60, i8 70, i8 80]

define ptr @p(ptr %x) { ret ptr %x }
define i16 @w(i16 %x) { ret i16 %x }

; Takes IY itself, as any compiled function may.
define i16 @sum3(ptr %q) {
  %a = getelementptr i8, ptr %q, i16 5
  %b = getelementptr i8, ptr %q, i16 6
  %c = getelementptr i8, ptr %q, i16 7
  %x = load i8, ptr %a
  %y = load i8, ptr %b
  %z = load i8, ptr %c
  %xw = zext i8 %x to i16
  %yw = zext i8 %y to i16
  %zw = zext i8 %z to i16
  %s = add i16 %xw, %yw
  %r = add i16 %s, %zw
  ret i16 %r }

; test fields_read hl=18
define i16 @fields_read() { %q = call ptr @p(ptr @bytes)
  %r = call i16 @sum3(ptr %q)
  %b = getelementptr i8, ptr %q, i16 -0
  %f = load i8, ptr %b
  %fw = zext i8 %f to i16
  %t = sub i16 %r, %fw
  %u = sub i16 %t, 2
  ret i16 %u }

; test negative_offset a=6
define i8 @negative_offset() { %q = call ptr @p(ptr getelementptr (i8, ptr @bytes, i16 7))
  %a = getelementptr i8, ptr %q, i16 -3
  %b = getelementptr i8, ptr %q, i16 -7
  %x = load i8, ptr %a
  %y = load i8, ptr %b
  %r = add i8 %x, %y
  ret i8 %r }

; The furthest a 16-bit access reaches through a displacement, and past it.
; test edge_of_reach hl=1234
define i16 @edge_of_reach() { %q = call ptr @p(ptr @words)
  %a = getelementptr i8, ptr %q, i16 126
  %b = getelementptr i8, ptr %q, i16 127
  %c = getelementptr i8, ptr %q, i16 128
  store i16 1000, ptr %c
  store i8 34, ptr %b
  store i16 200, ptr %a
  %x = load i16, ptr %c
  %y = load i16, ptr %a
  %r = add i16 %x, %y
  %s = add i16 %r, 34
  %t = sub i16 %s, 34
  %u = add i16 %t, 34
  ret i16 %u }

; test store_then_read hl=772
define i16 @store_then_read() { %q = call ptr @p(ptr @words)
  %a = getelementptr i8, ptr %q, i16 2
  %b = getelementptr i8, ptr %q, i16 3
  store i8 4, ptr %a
  store i8 3, ptr %b
  %r = load i16, ptr %a
  ret i16 %r }

; Two pointers in turn, so IY cannot simply keep one.
; test two_in_turn hl=3211
define i16 @two_in_turn() { %q = call ptr @p(ptr @bytes)
  %o = call ptr @p(ptr @other)
  %a1 = getelementptr i8, ptr %q, i16 1
  %b1 = getelementptr i8, ptr %o, i16 1
  %a2 = getelementptr i8, ptr %q, i16 2
  %b2 = getelementptr i8, ptr %o, i16 2
  %x = load i8, ptr %a1
  %y = load i8, ptr %b1
  %z = load i8, ptr %a2
  %v = load i8, ptr %b2
  %xw = zext i8 %x to i16
  %yw = zext i8 %y to i16
  %zw = zext i8 %z to i16
  %vw = zext i8 %v to i16
  %m1 = mul i16 %yw, 100
  %m2 = mul i16 %vw, 10
  %m3 = mul i16 %zw, 300
  %s1 = add i16 %m1, %m2
  %s2 = add i16 %s1, %m3
  %s3 = add i16 %s2, %xw
  %s4 = add i16 %s3, 9
  ret i16 %s4 }

; A call between two fields: IY has to be set again.
; test call_between hl=24
define i16 @call_between() { %q = call ptr @p(ptr @bytes)
  %a = getelementptr i8, ptr %q, i16 1
  %b = getelementptr i8, ptr %q, i16 3
  %x = load i8, ptr %a
  %o = call ptr @p(ptr @other)
  %s = call i16 @sum3(ptr %o)
  %y = load i8, ptr %b
  %xw = zext i8 %x to i16
  %yw = zext i8 %y to i16
  %t = add i16 %xw, %yw
  %u = add i16 %t, 18
  %r = sub i16 %s, %s
  %v = add i16 %u, %r
  ret i16 %v }

; A multiply between two fields is a helper that leaves IY, and it must still be right.
; test helper_between hl=56
define i16 @helper_between() { %q = call ptr @p(ptr @bytes)
  %n = call i16 @w(i16 7)
  %a = getelementptr i8, ptr %q, i16 1
  %b = getelementptr i8, ptr %q, i16 3
  %x = load i8, ptr %a
  %xw = zext i8 %x to i16
  %m = mul i16 %n, %xw
  %y = load i8, ptr %b
  %yw = zext i8 %y to i16
  %r = mul i16 %m, %yw
  ret i16 %r }

; The pointer a different value each pass of the loop, IY's last value left over from the pass before.
; test walks_a_loop hl=26
define i16 @walks_a_loop() { %q = call ptr @p(ptr @bytes)
  br label %head
head:
  %at = phi ptr [ %q, %0 ], [ %next, %head ]
  %s = phi i16 [ 0, %0 ], [ %s2, %head ]
  %i = phi i16 [ 0, %0 ], [ %i2, %head ]
  %a = getelementptr i8, ptr %at, i16 1
  %x = load i8, ptr %at
  %y = load i8, ptr %a
  %xw = zext i8 %x to i16
  %yw = zext i8 %y to i16
  %s1 = add i16 %s, %xw
  %s2 = add i16 %s1, %yw
  %next = getelementptr i8, ptr %at, i16 2
  %i2 = add i16 %i, 1
  %done = icmp eq i16 %i2, 3
  br i1 %done, label %out, label %head
out:
  %r = add i16 %s2, 5
  ret i16 %r }

; IY holds %q round the loop except on the pass that calls, which takes it.
; test held_round_a_loop hl=109
define i16 @held_round_a_loop() { %q = call ptr @p(ptr @bytes)
  %a = getelementptr i8, ptr %q, i16 1
  %b = getelementptr i8, ptr %q, i16 2
  br label %head
head:
  %s = phi i16 [ 0, %0 ], [ %s3, %join ]
  %i = phi i16 [ 0, %0 ], [ %i2, %join ]
  %x = load i8, ptr %a
  %xw = zext i8 %x to i16
  %s1 = add i16 %s, %xw
  %odd = and i16 %i, 1
  %c = icmp eq i16 %odd, 1
  br i1 %c, label %calls, label %join
calls:
  %o = call ptr @p(ptr @other)
  %k = call i16 @sum3(ptr %o)
  %kk = sub i16 %k, 180
  br label %join
join:
  %add = phi i16 [ 0, %head ], [ %kk, %calls ]
  %y = load i8, ptr %b
  %yw = zext i8 %y to i16
  %s2 = add i16 %s1, %yw
  %s3 = add i16 %s2, %add
  %i2 = add i16 %i, 1
  %done = icmp eq i16 %i2, 4
  br i1 %done, label %out, label %head
out:
  %r = add i16 %s3, 29
  ret i16 %r }

; Either of two pointers, known only after the join.
; test join_of_two a=70
define i8 @join_of_two() { %n = call i16 @w(i16 1)
  %q = call ptr @p(ptr @bytes)
  %o = call ptr @p(ptr @other)
  %c = icmp eq i16 %n, 1
  br i1 %c, label %left, label %right
left:
  %la = getelementptr i8, ptr %q, i16 1
  %lx = load i8, ptr %la
  br label %join
right:
  %ra = getelementptr i8, ptr %o, i16 2
  %rx = load i8, ptr %ra
  br label %join
join:
  %t = phi ptr [ %o, %left ], [ %q, %right ]
  %v = phi i8 [ %lx, %left ], [ %rx, %right ]
  %a = getelementptr i8, ptr %t, i16 6
  %x = load i8, ptr %a
  %r = sub i8 %x, %v
  %s = add i8 %r, 2
  ret i8 %s }
