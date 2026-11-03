; no-host: ports mean nothing on the machine compiling this.
; z80run's ports answer 0xFF to a read and remember the last write.

; test outimm last_out=77
define void @outimm() {
  store volatile i8 77, ptr addrspace(1) inttoptr (i16 190 to ptr addrspace(1))
  ret void }
; test outvar last_out=78
define void @outvar() {
  %p = call i16 @w(i16 191)
  %q = inttoptr i16 %p to ptr addrspace(1)
  store volatile i8 78, ptr addrspace(1) %q
  ret void }
; test inimm a=255
define i8 @inimm() {
  %r = load volatile i8, ptr addrspace(1) inttoptr (i16 220 to ptr addrspace(1))
  ret i8 %r }
; test invar a=255
define i8 @invar() {
  %p = call i16 @w(i16 221)
  %q = inttoptr i16 %p to ptr addrspace(1)
  %r = load volatile i8, ptr addrspace(1) %q
  ret i8 %r }
define i16 @w(i16 %x) { ret i16 %x }

; test absolute a=42
define i8 @absolute() {
  store volatile i8 42, ptr inttoptr (i16 49152 to ptr)
  %r = load volatile i8, ptr inttoptr (i16 49152 to ptr)
  ret i8 %r }

; An object with lifetime markers, as the optimizer leaves after inlining: their operand must
; stay a frame index.
declare void @llvm.lifetime.start.p0(ptr)
declare void @llvm.lifetime.end.p0(ptr)
define void @fill(ptr %p) { store i16 77, ptr %p
  ret void }
; test lifetimes hl=77
define i16 @lifetimes() {
  %a = alloca i16
  call void @llvm.lifetime.start.p0(ptr %a)
  call void @fill(ptr %a)
  %r = load i16, ptr %a
  call void @llvm.lifetime.end.p0(ptr %a)
  ret i16 %r }

; The checked multiplies' overflow flag is an i16: what sits beside the slot LLVM makes for it
; must be left alone. (no-host: these are the Z80 runtime's own functions.)
declare i32 @__mulosi4(i32, i32, ptr)
declare i64 @__mulodi4(i64, i64, ptr)
define i32 @dd(i32 %x) { ret i32 %x }
define i64 @qq(i64 %x) { ret i64 %x }
; test flagsize32 hl=4660
define i16 @flagsize32() {
  %slots = alloca [2 x i16]
  %flag = getelementptr [2 x i16], ptr %slots, i16 0, i16 0
  %guard = getelementptr [2 x i16], ptr %slots, i16 0, i16 1
  store volatile i16 4660, ptr %guard
  %x = call i32 @dd(i32 100000)
  %r = call i32 @__mulosi4(i32 %x, i32 100000, ptr %flag)
  %g = load volatile i16, ptr %guard
  ret i16 %g }
; test flagset32 hl=1
define i16 @flagset32() {
  %slots = alloca [2 x i16]
  %flag = getelementptr [2 x i16], ptr %slots, i16 0, i16 0
  %x = call i32 @dd(i32 100000)
  %r = call i32 @__mulosi4(i32 %x, i32 100000, ptr %flag)
  %f = load volatile i16, ptr %flag
  ret i16 %f }
; test flagsize64 hl=4660
define i16 @flagsize64() {
  %slots = alloca [2 x i16]
  %flag = getelementptr [2 x i16], ptr %slots, i16 0, i16 0
  %guard = getelementptr [2 x i16], ptr %slots, i16 0, i16 1
  store volatile i16 4660, ptr %guard
  %x = call i64 @qq(i64 5000000000)
  %r = call i64 @__mulodi4(i64 %x, i64 5000000000, ptr %flag)
  %g = load volatile i16, ptr %guard
  ret i16 %g }
