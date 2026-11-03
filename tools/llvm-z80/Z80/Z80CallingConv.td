//===-- Z80CallingConv.td - Z80 calling convention ---------------*- tablegen -*-===//
//
// Two 16-bit values (or the low bytes of those pairs) in registers, the rest on the stack.
// BC stays free for an indirect call's target. An i32 is its two halves: HL low, DE high.
//
//===----------------------------------------------------------------------===//

def CC_Z80 : CallingConv<[
  CCIfType<[i1], CCPromoteToType<i8>>,
  CCIfType<[i16], CCAssignToRegWithShadow<[HL, DE], [L, E]>>,
  CCIfType<[i8], CCAssignToRegWithShadow<[L, E], [HL, DE]>>,
  CCIfType<[i8], CCAssignToStack<1, 1>>,
  CCIfType<[i16], CCAssignToStack<2, 1>>
]>;

def RetCC_Z80 : CallingConv<[
  CCIfType<[i1], CCPromoteToType<i8>>,
  CCIfType<[i8], CCAssignToReg<[A]>>,
  CCIfType<[i16], CCAssignToReg<[HL, DE, BC]>>
]>;

def CSR_Z80 : CalleeSavedRegs<(add)>;
