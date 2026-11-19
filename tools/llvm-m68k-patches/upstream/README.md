# Patches for LLVM main, not the pin

`tools/llvm-m68k.sh` applies `../*.patch` to the pinned LLVM (e7dd336e0f78). The patches here
are for LLVM **main**, past the pin, and the script does not apply them. Each fixes something
main got wrong after the pin. It is offered upstream, and is needed here only once the pin
moves past the change it answers.

## `m68k-no-clr-for-volatile-stores.patch`

**What it fixes.** 16c337766166 ("[M68k] Implement `CLR` instruction encoding and logic",
#216636, 2026-08-25) selects every store of zero to memory as `CLR`. On the 68000 (and the
68008), `CLR` reads its destination before writing it, which the commit's own comment notes.
For a volatile store that read is a side effect. Reading a memory-mapped device register can
change its state. On the Sega Genesis, `clr.w $c00004` reads the VDP's control port, which
drops a half-written command, and every ROM's screen goes black. That is why the pin stays
before that commit.

**The fix.** `MxClrStore`, a `PatFrag` over `store`, matches unless the store is volatile and
the CPU is a 68000 (`!Subtarget->atLeastM68010()`). Both of `CLR`'s memory forms use it
(`MxClr_M`, `MxClr_B`), so a volatile zero there is a `MOVE` of zero, as before 16c337766166.
A plain store, and any store on the 68010 and later, is still `CLR`. The test is
`llvm/test/CodeGen/M68k/Data/store-zero-volatile.ll`: bytes, words and longs, through a
pointer and to a global, on the 68000 and the 68010.

**Checked on main fffc865e3aaa** (2026-11-19), building `llc` with
`-DLLVM_EXPERIMENTAL_TARGETS_TO_BUILD=M68k`:
- `llvm-lit llvm/test/CodeGen/M68k` passes all but `pipeline.ll` and
  `is-pcrel-register-operand-legal.mir`, which fail on main without the patch too.
- The new test fails without the patch.

**Not yet sent upstream.** This is the text drafted for a report and pull request:

> **[M68k] Do not select CLR for volatile stores on the 68000**
>
> Since #216636, a store of zero to memory is selected as `CLR`. On the MC68000, `CLR` performs
> a read of its destination before the write (as the comment on `MxClr_M` notes). That is
> harmless for ordinary memory, but wrong for a volatile store: reading a memory-mapped I/O
> register can have side effects. On the Sega Genesis, for example, a read of the VDP control
> port at `$C00004` aborts a pending two-word command, so `store volatile i16 0` to it now
> breaks the video setup of every program.
>
> ```llvm
> define void @f() {
>   store volatile i16 0, ptr inttoptr (i32 12582916 to ptr)
>   ret void
> }
> ```
>
> `llc -mtriple=m68k -mcpu=M68000` gives `move.l #12582916, %a0; clr.w (%a0)`. Before #216636
> it gave `move.w #0, $c00004`.
>
> This patch adds a `PatFrag` used by `CLR`'s memory patterns that refuses a volatile store
> when the subtarget is below the 68010, whose `CLR` no longer reads its destination. Plain
> stores keep `CLR` everywhere. Test: `CodeGen/M68k/Data/store-zero-volatile.ll`.
