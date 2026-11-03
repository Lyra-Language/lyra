; Each `; test NAME REGISTER=VALUE`: NAME called with no arguments, then the register read.
; Values reach them through calls (llc does not inline), so that the
; optimizer cannot answer a test before the Z80 does.

@g8 = global i8 7
@g16 = global i16 1000
@table = constant [4 x i16] [i16 11, i16 22, i16 33, i16 44]
@buf = global [8 x i8] zeroinitializer

define i8 @b(i8 %x) { ret i8 %x }
define i16 @w(i16 %x) { ret i16 %x }
define i32 @d(i32 %x) { ret i32 %x }
define i64 @q(i64 %x) { ret i64 %x }

; test add8 a=12
define i8 @add8() { %x = call i8 @b(i8 5)
  %y = call i8 @b(i8 7)
  %r = add i8 %x, %y
  ret i8 %r }
; test sub8 a=254
define i8 @sub8() { %x = call i8 @b(i8 5)
  %y = call i8 @b(i8 7)
  %r = sub i8 %x, %y
  ret i8 %r }
; test logic8 a=125
define i8 @logic8() { %x = call i8 @b(i8 240)
  %y = call i8 @b(i8 60)
  %a = and i8 %x, %y
  %o = or i8 %a, 85
  %r = xor i8 %o, 8
  ret i8 %r }
; test add16 hl=46912
define i16 @add16() { %x = call i16 @w(i16 12345)
  %y = call i16 @w(i16 34567)
  %r = add i16 %x, %y
  ret i16 %r }
; test sub16 hl=63314
define i16 @sub16() { %x = call i16 @w(i16 100)
  %y = call i16 @w(i16 2322)
  %r = sub i16 %x, %y
  ret i16 %r }
; test logic16 hl=8180
define i16 @logic16() { %x = call i16 @w(i16 61680)
  %y = call i16 @w(i16 4080)
  %a = and i16 %x, %y
  %z = xor i16 %a, 3840
  %r = or i16 %z, 4660
  ret i16 %r }
; test shl16 hl=19456
define i16 @shl16() { %x = call i16 @w(i16 19)
  %r = shl i16 %x, 10
  ret i16 %r }
; test lshr16 hl=4096
define i16 @lshr16() { %x = call i16 @w(i16 32768)
  %r = lshr i16 %x, 3
  ret i16 %r }
; test ashr16 hl=61440
define i16 @ashr16() { %x = call i16 @w(i16 32768)
  %r = ashr i16 %x, 3
  ret i16 %r }
; test shl8 a=80
define i8 @shl8() { %x = call i8 @b(i8 5)
  %r = shl i8 %x, 4
  ret i8 %r }
; test ashr8 a=248
define i8 @ashr8() { %x = call i8 @b(i8 128)
  %r = ashr i8 %x, 4
  ret i8 %r }
; test varshift hl=320
define i16 @varshift() { %x = call i16 @w(i16 5)
  %n = call i8 @b(i8 6)
  %wn = zext i8 %n to i16
  %r = shl i16 %x, %wn
  ret i16 %r }
; test varshift8 a=12
define i8 @varshift8() { %x = call i8 @b(i8 192)
  %n = call i8 @b(i8 4)
  %r = lshr i8 %x, %n
  ret i8 %r }
; test mul16 hl=56088
define i16 @mul16() { %x = call i16 @w(i16 123)
  %y = call i16 @w(i16 456)
  %r = mul i16 %x, %y
  ret i16 %r }
; test mul8 a=56
define i8 @mul8() { %x = call i8 @b(i8 12)
  %y = call i8 @b(i8 90)
  %r = mul i8 %x, %y
  ret i8 %r }
; test udiv16 hl=406
define i16 @udiv16() { %x = call i16 @w(i16 56088)
  %y = call i16 @w(i16 138)
  %r = udiv i16 %x, %y
  ret i16 %r }
; test sdiv16 hl=65526
define i16 @sdiv16() { %x = call i16 @w(i16 -100)
  %y = call i16 @w(i16 10)
  %r = sdiv i16 %x, %y
  ret i16 %r }
; test srem16 hl=65534
define i16 @srem16() { %x = call i16 @w(i16 -102)
  %y = call i16 @w(i16 10)
  %r = srem i16 %x, %y
  ret i16 %r }
; test zext hl=200
define i16 @zext() { %x = call i8 @b(i8 200)
  %r = zext i8 %x to i16
  ret i16 %r }
; test sext hl=65080
define i16 @sext() { %x = call i8 @b(i8 56)
  %y = sub i8 %x, 256
  %z = add i8 %y, 200
  %m = sext i8 %z to i16
  %r = add i16 %m, 65080
  ret i16 %r }
; test trunc a=52
define i8 @trunc() { %x = call i16 @w(i16 4660)
  %r = trunc i16 %x to i8
  ret i8 %r }

; Each comparison both ways round its boundary, made into a bit mask.
define i16 @cmps(i16 %a, i16 %b) {
  %c0 = icmp eq i16 %a, %b
  %c1 = icmp ne i16 %a, %b
  %c2 = icmp ult i16 %a, %b
  %c3 = icmp ule i16 %a, %b
  %c4 = icmp ugt i16 %a, %b
  %c5 = icmp uge i16 %a, %b
  %c6 = icmp slt i16 %a, %b
  %c7 = icmp sle i16 %a, %b
  %c8 = icmp sgt i16 %a, %b
  %c9 = icmp sge i16 %a, %b
  %r0 = select i1 %c0, i16 1, i16 0
  %r1 = select i1 %c1, i16 2, i16 0
  %r2 = select i1 %c2, i16 4, i16 0
  %r3 = select i1 %c3, i16 8, i16 0
  %r4 = select i1 %c4, i16 16, i16 0
  %r5 = select i1 %c5, i16 32, i16 0
  %r6 = select i1 %c6, i16 64, i16 0
  %r7 = select i1 %c7, i16 128, i16 0
  %r8 = select i1 %c8, i16 256, i16 0
  %r9 = select i1 %c9, i16 512, i16 0
  %a0 = or i16 %r0, %r1
  %a1 = or i16 %a0, %r2
  %a2 = or i16 %a1, %r3
  %a3 = or i16 %a2, %r4
  %a4 = or i16 %a3, %r5
  %a5 = or i16 %a4, %r6
  %a6 = or i16 %a5, %r7
  %a7 = or i16 %a6, %r8
  %a8 = or i16 %a7, %r9
  ret i16 %a8 }
; test cmp_equal hl=681
define i16 @cmp_equal() { %r = call i16 @cmps(i16 5, i16 5)
  ret i16 %r }
; test cmp_less hl=206
define i16 @cmp_less() { %r = call i16 @cmps(i16 5, i16 6)
  ret i16 %r }
; test cmp_more hl=818
define i16 @cmp_more() { %r = call i16 @cmps(i16 6, i16 5)
  ret i16 %r }
; a is negative: below b signed, above it unsigned.
; test cmp_sign hl=242
define i16 @cmp_sign() { %r = call i16 @cmps(i16 -1, i16 1)
  ret i16 %r }
; test cmp_min hl=242
define i16 @cmp_min() { %r = call i16 @cmps(i16 -32768, i16 32767)
  ret i16 %r }
; test cmp8 a=3
define i8 @cmp8() { %x = call i8 @b(i8 200)
  %y = call i8 @b(i8 100)
  %u = icmp ugt i8 %x, %y
  %s = icmp slt i8 %x, %y
  %ru = select i1 %u, i8 1, i8 0
  %rs = select i1 %s, i8 2, i8 0
  %r = or i8 %ru, %rs
  ret i8 %r }
; test cmp32 hl=3
define i16 @cmp32() { %x = call i32 @d(i32 65536)
  %y = call i32 @d(i32 65535)
  %u = icmp ugt i32 %x, %y
  %s = icmp sgt i32 %x, %y
  %ru = select i1 %u, i16 1, i16 0
  %rs = select i1 %s, i16 2, i16 0
  %r = or i16 %ru, %rs
  ret i16 %r }

; test add32lo hl=0
; test add32hi de=1
define i32 @add32() { %x = call i32 @d(i32 65535)
  %y = call i32 @d(i32 1)
  %r = add i32 %x, %y
  ret i32 %r }
define i32 @add32lo() { %r = call i32 @add32()
  ret i32 %r }
define i32 @add32hi() { %r = call i32 @add32()
  ret i32 %r }
; test sub32 hl=65535
define i16 @sub32() { %x = call i32 @d(i32 65536)
  %y = call i32 @d(i32 1)
  %r = sub i32 %x, %y
  %t = trunc i32 %r to i16
  ret i16 %t }
; test shl32 de=5
define i32 @shl32() { %x = call i32 @d(i32 5)
  %r = shl i32 %x, 24
  %s = lshr i32 %r, 8
  ret i32 %s }
; test add64 hl=4
define i16 @add64() { %x = call i64 @q(i64 4294967295)
  %y = call i64 @q(i64 5)
  %r = add i64 %x, %y
  %t = trunc i64 %r to i16
  ret i16 %t }
; test add64hi hl=1
define i16 @add64hi() { %x = call i64 @q(i64 4294967295)
  %y = call i64 @q(i64 5)
  %r = add i64 %x, %y
  %s = lshr i64 %r, 32
  %t = trunc i64 %s to i16
  ret i16 %t }

; test global8 a=9
define i8 @global8() { %x = load i8, ptr @g8
  %y = add i8 %x, 2
  store i8 %y, ptr @g8
  %r = load volatile i8, ptr @g8
  store i8 7, ptr @g8
  ret i8 %r }
; test global16 hl=1001
define i16 @global16() { %x = load i16, ptr @g16
  %y = add i16 %x, 1
  store volatile i16 %y, ptr @g16
  %r = load volatile i16, ptr @g16
  store i16 1000, ptr @g16
  ret i16 %r }
; test tableidx hl=33
define i16 @tableidx() { %i = call i16 @w(i16 2)
  %p = getelementptr [4 x i16], ptr @table, i16 0, i16 %i
  %r = load i16, ptr %p
  ret i16 %r }
; test bytes a=30
define i8 @bytes() {
  %i = call i16 @w(i16 3)
  %p = getelementptr [8 x i8], ptr @buf, i16 0, i16 %i
  store i8 30, ptr %p
  %r = load volatile i8, ptr %p
  ret i8 %r }

; A local array, through its address and a variable index.
; test local hl=60
define i16 @local() {
  %a = alloca [4 x i16]
  %i0 = getelementptr [4 x i16], ptr %a, i16 0, i16 0
  store volatile i16 10, ptr %i0
  %i1 = getelementptr [4 x i16], ptr %a, i16 0, i16 1
  store volatile i16 20, ptr %i1
  %i2 = getelementptr [4 x i16], ptr %a, i16 0, i16 2
  store volatile i16 30, ptr %i2
  %n = call i16 @w(i16 2)
  %p = getelementptr [4 x i16], ptr %a, i16 0, i16 %n
  %x = load volatile i16, ptr %p
  %y = load volatile i16, ptr %i0
  %z = load volatile i16, ptr %i1
  %s = add i16 %x, %y
  %r = add i16 %s, %z
  ret i16 %r }

; Arguments past the registers, of both sizes.
define i16 @many(i16 %a, i16 %b, i16 %c, i8 %d, i16 %e) {
  %wd = zext i8 %d to i16
  %s1 = sub i16 %a, %b
  %s2 = sub i16 %s1, %c
  %s3 = sub i16 %s2, %wd
  %s4 = sub i16 %s3, %e
  ret i16 %s4 }
; test stackargs hl=234
define i16 @stackargs() { %r = call i16 @many(i16 1000, i16 300, i16 200, i8 16, i16 250)
  ret i16 %r }
define i8 @two(i8 %a, i8 %b) { %r = sub i8 %a, %b
  ret i8 %r }
; test byteargs a=3
define i8 @byteargs() { %r = call i8 @two(i8 10, i8 7)
  ret i8 %r }

; test loop hl=5050
define i16 @loop() { %n = call i16 @w(i16 100)
  br label %head
head:
  %i = phi i16 [ 1, %0 ], [ %i2, %head ]
  %s = phi i16 [ 0, %0 ], [ %s2, %head ]
  %s2 = add i16 %s, %i
  %i2 = add i16 %i, 1
  %more = icmp ule i16 %i2, %n
  br i1 %more, label %head, label %out
out:
  ret i16 %s2 }

define i16 @fib(i16 %n) {
  %small = icmp ult i16 %n, 2
  br i1 %small, label %base, label %rec
base:
  ret i16 %n
rec:
  %n1 = sub i16 %n, 1
  %n2 = sub i16 %n, 2
  %a = call i16 @fib(i16 %n1)
  %b = call i16 @fib(i16 %n2)
  %r = add i16 %a, %b
  ret i16 %r }
; test recursion hl=610
define i16 @recursion() { %r = call i16 @fib(i16 15)
  ret i16 %r }

define i16 @inc(i16 %x) { %r = add i16 %x, 1
  ret i16 %r }
define i16 @dec(i16 %x) { %r = sub i16 %x, 1
  ret i16 %r }
; Indirect, with the target chosen at run time and an argument beside it.
; test indirect hl=41
define i16 @indirect() {
  %k = call i16 @w(i16 1)
  %z = icmp eq i16 %k, 0
  %f = select i1 %z, ptr @inc, ptr @dec
  %r = call i16 %f(i16 42)
  ret i16 %r }

; More live values than the registers hold, so spills, and values live across a call.
; test pressure hl=118
define i16 @pressure() {
  %a = call i16 @w(i16 1)
  %b = call i16 @w(i16 2)
  %c = call i16 @w(i16 3)
  %d = call i16 @w(i16 4)
  %e = call i16 @w(i16 5)
  %f = call i16 @w(i16 6)
  %g = call i16 @w(i16 7)
  %h = call i16 @w(i16 8)
  %s1 = add i16 %a, %b
  %s2 = add i16 %c, %d
  %s3 = add i16 %e, %f
  %s4 = add i16 %g, %h
  %t1 = add i16 %s1, %s2
  %t2 = add i16 %s3, %s4
  %u = add i16 %t1, %t2
  %v = add i16 %u, %a
  %w1 = add i16 %v, %b
  %x = add i16 %w1, %c
  %y = add i16 %x, %d
  %z = add i16 %y, %e
  %aa = add i16 %z, %f
  %ab = add i16 %aa, %g
  %ac = add i16 %ab, %h
  %ad = add i16 %ac, %s1
  %ae = add i16 %ad, %s2
  %af = add i16 %ae, %s3
  %ag = add i16 %af, %s4
  %r = add i16 %ag, %t1
  ret i16 %r }

; A checked add, as Lyra emits one.
; test checked hl=1
define i16 @checked() {
  %x = call i16 @w(i16 32767)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r }
; test unchecked hl=0
define i16 @unchecked() {
  %x = call i16 @w(i16 32766)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r }
declare { i16, i1 } @llvm.sadd.with.overflow.i16(i16, i16)

; A struct returned through a pointer, and memcpy.
%pair = type { i16, i8, i16 }
define void @makepair(ptr sret(%pair) %out, i16 %x) {
  store i16 %x, ptr %out
  %b = getelementptr %pair, ptr %out, i16 0, i32 1
  store i8 9, ptr %b
  %c = getelementptr %pair, ptr %out, i16 0, i32 2
  %y = add i16 %x, 1
  store i16 %y, ptr %c
  ret void }
; test sret hl=510
define i16 @sret() {
  %p = alloca %pair
  call void @makepair(ptr sret(%pair) %p, i16 250)
  %q = alloca %pair
  call void @llvm.memcpy.p0.p0.i16(ptr %q, ptr %p, i16 5, i1 false)
  %x = load i16, ptr %q
  %b = getelementptr %pair, ptr %q, i16 0, i32 1
  %m = load i8, ptr %b
  %wm = zext i8 %m to i16
  %c = getelementptr %pair, ptr %q, i16 0, i32 2
  %y = load i16, ptr %c
  %s = add i16 %x, %y
  %r = add i16 %s, %wm
  ret i16 %r }
declare void @llvm.memcpy.p0.p0.i16(ptr, ptr, i16, i1)

; A local larger than a displacement reaches: slots past (ix+127), both ways, spilled values
; around them, and a compare's flags surviving the access.
; test farslots hl=1234
define i16 @farslots() {
  %big = alloca [100 x i16]
  %last = alloca i16
  %far = alloca [100 x i16]
  %p0 = getelementptr [100 x i16], ptr %big, i16 0, i16 99
  %p1 = getelementptr [100 x i16], ptr %far, i16 0, i16 99
  store volatile i16 1000, ptr %p0
  store volatile i16 234, ptr %p1
  store volatile i16 7, ptr %last
  %a = load volatile i16, ptr %p0
  %b = load volatile i16, ptr %p1
  %r = add i16 %a, %b
  ret i16 %r }

; The memory helpers, each through what the optimizer calls them from: large enough not to be
; expanded in place, and each checked at both ends so a wrong length shows.
@region = global [80 x i8] zeroinitializer
declare void @llvm.memset.p0.i16(ptr, i8, i16, i1)
declare void @llvm.memmove.p0.p0.i16(ptr, ptr, i16, i1)
declare i32 @memcmp(ptr, ptr, i16)
; test memsetends hl=12345
define i16 @memsetends() {
  %guard = getelementptr [80 x i8], ptr @region, i16 0, i16 60
  store i8 77, ptr %guard
  call void @llvm.memset.p0.i16(ptr @region, i8 9, i16 60, i1 false)
  %first = load i8, ptr @region
  %p = getelementptr [80 x i8], ptr @region, i16 0, i16 59
  %last = load i8, ptr %p
  %after = load i8, ptr %guard
  %a = icmp eq i8 %first, 9
  %b = icmp eq i8 %last, 9
  %c = icmp eq i8 %after, 77
  %ab = and i1 %a, %b
  %abc = and i1 %ab, %c
  %r = select i1 %abc, i16 12345, i16 1
  ret i16 %r }
; test memmoveup hl=23456
define i16 @memmoveup() {
  store i8 1, ptr @region
  %s1 = getelementptr [80 x i8], ptr @region, i16 0, i16 1
  store i8 2, ptr %s1
  %s39 = getelementptr [80 x i8], ptr @region, i16 0, i16 39
  store i8 3, ptr %s39
  ; Overlapping: the destination above the source, so it must run from the top.
  call void @llvm.memmove.p0.p0.i16(ptr %s1, ptr @region, i16 39, i1 false)
  %x = load i8, ptr %s1
  %s2 = getelementptr [80 x i8], ptr @region, i16 0, i16 2
  %y = load i8, ptr %s2
  %a = icmp eq i8 %x, 1
  %b = icmp eq i8 %y, 2
  %ab = and i1 %a, %b
  %r = select i1 %ab, i16 23456, i16 1
  ret i16 %r }
; test memcmpsays hl=3
define i16 @memcmpsays() {
  store i8 5, ptr @region
  %s10 = getelementptr [80 x i8], ptr @region, i16 0, i16 10
  store i8 5, ptr %s10
  %same = call i32 @memcmp(ptr @region, ptr %s10, i16 1)
  store i8 6, ptr %s10
  %less = call i32 @memcmp(ptr @region, ptr %s10, i16 1)
  %a = icmp eq i32 %same, 0
  %b = icmp slt i32 %less, 0
  %ra = select i1 %a, i16 1, i16 0
  %rb = select i1 %b, i16 2, i16 0
  %r = or i16 %ra, %rb
  ret i16 %r }

; Checked arithmetic, each kind at its edge and just inside it, both as the jump a check makes
; and as a value. %w is the result returned when it did not overflow, 999 when it did.
; test jump0 hl=999
define i16 @jump0() {
  %x = call i16 @w(i16 32767)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value0 hl=1
define i16 @value0() {
  %x = call i16 @w(i16 32767)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump1 hl=32767
define i16 @jump1() {
  %x = call i16 @w(i16 32766)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value1 hl=0
define i16 @value1() {
  %x = call i16 @w(i16 32766)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump2 hl=999
define i16 @jump2() {
  %x = call i16 @w(i16 -32768)
  %y = call i16 @w(i16 -1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value2 hl=1
define i16 @value2() {
  %x = call i16 @w(i16 -32768)
  %y = call i16 @w(i16 -1)
  %o = call { i16, i1 } @llvm.sadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump3 hl=999
define i16 @jump3() {
  %x = call i16 @w(i16 -32768)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.ssub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value3 hl=1
define i16 @value3() {
  %x = call i16 @w(i16 -32768)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.ssub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump4 hl=32768
define i16 @jump4() {
  %x = call i16 @w(i16 -32767)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.ssub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value4 hl=0
define i16 @value4() {
  %x = call i16 @w(i16 -32767)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.ssub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump5 hl=999
define i16 @jump5() {
  %x = call i16 @w(i16 32767)
  %y = call i16 @w(i16 -1)
  %o = call { i16, i1 } @llvm.ssub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value5 hl=1
define i16 @value5() {
  %x = call i16 @w(i16 32767)
  %y = call i16 @w(i16 -1)
  %o = call { i16, i1 } @llvm.ssub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump6 hl=999
define i16 @jump6() {
  %x = call i16 @w(i16 65535)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.uadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value6 hl=1
define i16 @value6() {
  %x = call i16 @w(i16 65535)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.uadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump7 hl=65535
define i16 @jump7() {
  %x = call i16 @w(i16 65534)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.uadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value7 hl=0
define i16 @value7() {
  %x = call i16 @w(i16 65534)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.uadd.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump8 hl=999
define i16 @jump8() {
  %x = call i16 @w(i16 0)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.usub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value8 hl=1
define i16 @value8() {
  %x = call i16 @w(i16 0)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.usub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump9 hl=0
define i16 @jump9() {
  %x = call i16 @w(i16 1)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.usub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i16, i1 } %o, 0
  %w = add i16 %v, 0
  ret i16 %w
}
; test value9 hl=0
define i16 @value9() {
  %x = call i16 @w(i16 1)
  %y = call i16 @w(i16 1)
  %o = call { i16, i1 } @llvm.usub.with.overflow.i16(i16 %x, i16 %y)
  %f = extractvalue { i16, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump10 hl=999
define i16 @jump10() {
  %x = call i8 @b(i8 127)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.sadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value10 hl=1
define i16 @value10() {
  %x = call i8 @b(i8 127)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.sadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump11 hl=127
define i16 @jump11() {
  %x = call i8 @b(i8 126)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.sadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value11 hl=0
define i16 @value11() {
  %x = call i8 @b(i8 126)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.sadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump12 hl=999
define i16 @jump12() {
  %x = call i8 @b(i8 -128)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.ssub.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value12 hl=1
define i16 @value12() {
  %x = call i8 @b(i8 -128)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.ssub.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump13 hl=999
define i16 @jump13() {
  %x = call i8 @b(i8 255)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.uadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value13 hl=1
define i16 @value13() {
  %x = call i8 @b(i8 255)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.uadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump14 hl=255
define i16 @jump14() {
  %x = call i8 @b(i8 254)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.uadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value14 hl=0
define i16 @value14() {
  %x = call i8 @b(i8 254)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.uadd.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump15 hl=999
define i16 @jump15() {
  %x = call i8 @b(i8 0)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.usub.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value15 hl=1
define i16 @value15() {
  %x = call i8 @b(i8 0)
  %y = call i8 @b(i8 1)
  %o = call { i8, i1 } @llvm.usub.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
; test jump16 hl=0
define i16 @jump16() {
  %x = call i8 @b(i8 5)
  %y = call i8 @b(i8 5)
  %o = call { i8, i1 } @llvm.usub.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  br i1 %f, label %over, label %fine
over:
  ret i16 999
fine:
  %v = extractvalue { i8, i1 } %o, 0
  %w = zext i8 %v to i16
  ret i16 %w
}
; test value16 hl=0
define i16 @value16() {
  %x = call i8 @b(i8 5)
  %y = call i8 @b(i8 5)
  %o = call { i8, i1 } @llvm.usub.with.overflow.i8(i8 %x, i8 %y)
  %f = extractvalue { i8, i1 } %o, 1
  %r = zext i1 %f to i16
  ret i16 %r
}
declare { i8, i1 } @llvm.sadd.with.overflow.i8(i8, i8)
declare { i16, i1 } @llvm.ssub.with.overflow.i16(i16, i16)
declare { i8, i1 } @llvm.ssub.with.overflow.i8(i8, i8)
declare { i16, i1 } @llvm.uadd.with.overflow.i16(i16, i16)
declare { i8, i1 } @llvm.uadd.with.overflow.i8(i8, i8)
declare { i16, i1 } @llvm.usub.with.overflow.i16(i16, i16)
declare { i8, i1 } @llvm.usub.with.overflow.i8(i8, i8)

; Counters wider than they need be, as Lyra's `for` makes them: a table's index, the counter's
; own value wanted wide, and a bound the compiler cannot narrow past 16 bits.
@squares = global [10 x i16] [i16 0, i16 1, i16 4, i16 9, i16 16, i16 25, i16 36, i16 49, i16 64, i16 81]
; test narrowindex hl=285
define i16 @narrowindex() {
  br label %head
head:
  %i = phi i64 [ 0, %0 ], [ %next, %head ]
  %s = phi i16 [ 0, %0 ], [ %s2, %head ]
  %t = trunc i64 %i to i16
  %p = getelementptr [10 x i16], ptr @squares, i16 0, i16 %t
  %v = load i16, ptr %p
  %s2 = add i16 %s, %v
  %next = add nuw nsw i64 %i, 1
  %more = icmp ult i64 %i, 9
  br i1 %more, label %head, label %out
out:
  ret i16 %s2 }
; test narrowwide hl=55
define i16 @narrowwide() {
  br label %head
head:
  %i = phi i64 [ 1, %0 ], [ %next, %head ]
  %s = phi i64 [ 0, %0 ], [ %s2, %head ]
  %s2 = add i64 %s, %i
  %next = add nuw nsw i64 %i, 1
  %more = icmp ult i64 %next, 11
  br i1 %more, label %head, label %out
out:
  %t = trunc i64 %s2 to i16
  ret i16 %t }
; A counter past what an i16 holds stays as it is.
; test widecount hl=4464
define i16 @widecount() {
  br label %head
head:
  %i = phi i32 [ 69000, %0 ], [ %next, %head ]
  %next = add nuw nsw i32 %i, 1000
  %more = icmp ult i32 %next, 70000
  br i1 %more, label %head, label %out
out:
  %t = trunc i32 %next to i16
  ret i16 %t }
; test signedzero hl=7
define i16 @signedzero() {
  %x = call i16 @w(i16 -5)
  %y = call i16 @w(i16 5)
  %z = call i16 @w(i16 0)
  %a = icmp slt i16 %x, 0
  %b = icmp sge i16 %y, 0
  %c = icmp eq i16 %z, 0
  %ra = select i1 %a, i16 1, i16 0
  %rb = select i1 %b, i16 2, i16 0
  %rc = select i1 %c, i16 4, i16 0
  %ab = or i16 %ra, %rb
  %r = or i16 %ab, %rc
  ret i16 %r }
; test bigshifts hl=4660
define i16 @bigshifts() {
  %x = call i16 @w(i16 4660)
  %h = lshr i16 %x, 8
  %hh = shl i16 %h, 8
  %s = call i16 @w(i16 -32768)
  %sa = ashr i16 %s, 12
  %sb = lshr i16 %sa, 12
  %m = and i16 %x, 255
  %r0 = or i16 %hh, %m
  %ok = icmp eq i16 %sb, 15
  %r = select i1 %ok, i16 %r0, i16 0
  ret i16 %r }
