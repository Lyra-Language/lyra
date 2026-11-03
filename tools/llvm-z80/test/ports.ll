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
