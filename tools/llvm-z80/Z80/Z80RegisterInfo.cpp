#include "Z80RegisterInfo.h"
#include "Z80FrameLowering.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/ADT/BitVector.h"
#include "llvm/CodeGen/LiveIntervals.h"
#include "llvm/CodeGen/MachineFrameInfo.h"
#include "llvm/CodeGen/MachineFunction.h"
#include "llvm/CodeGen/MachineInstr.h"
#include "llvm/CodeGen/TargetInstrInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"
#include "llvm/IR/Function.h"
#include "llvm/Support/ErrorHandling.h"

#define GET_REGINFO_TARGET_DESC
#include "Z80GenRegisterInfo.inc"

using namespace llvm;

Z80RegisterInfo::Z80RegisterInfo() : Z80GenRegisterInfo(0) {}

const MCPhysReg *
Z80RegisterInfo::getCalleeSavedRegs(const MachineFunction *) const {
  return CSR_Z80_SaveList;
}

const uint32_t *Z80RegisterInfo::getCallPreservedMask(const MachineFunction &,
                                                      CallingConv::ID) const {
  return CSR_Z80_RegMask;
}

BitVector Z80RegisterInfo::getReservedRegs(const MachineFunction &) const {
  BitVector Reserved(getNumRegs());
  Reserved.set(Z80::SP);
  Reserved.set(Z80::IX);
  Reserved.set(Z80::IY);
  Reserved.set(Z80::F);
  return Reserved;
}

// IX is the stack pointer's value through a function's body, so a slot's displacement is from
// the frame's bottom: its offset from the entry SP, the frame's size, and the saved IX.
bool Z80RegisterInfo::eliminateFrameIndex(MachineBasicBlock::iterator II, int,
                                          unsigned FIOperandNum,
                                          RegScavenger *) const {
  MachineInstr &MI = *II;
  MachineFunction &MF = *MI.getParent()->getParent();
  const MachineFrameInfo &MFI = MF.getFrameInfo();
  int FI = MI.getOperand(FIOperandNum).getIndex();
  int64_t Disp = MFI.getObjectOffset(FI) + MFI.getStackSize() + 2 +
                 MI.getOperand(FIOperandNum + 1).getImm();
  // A 16-bit access reads the displacement and the one after it.
  bool Wide = MI.getOpcode() == Z80::LD16rf || MI.getOpcode() == Z80::ST16fr;
  if (MI.getOpcode() != Z80::LEA16 && (Disp < -128 || Disp + Wide > 127))
    report_fatal_error("Z80: a stack slot is out of IX's reach in " +
                       MF.getName());
  MI.getOperand(FIOperandNum).ChangeToRegister(Z80::IX, false);
  MI.getOperand(FIOperandNum + 1).setImm(Disp);
  return false;
}

Register Z80RegisterInfo::getFrameRegister(const MachineFunction &) const {
  return Z80::IX;
}

const TargetRegisterClass *Z80RegisterInfo::getPointerRegClass(unsigned) const {
  return &Z80::GR16RegClass;
}

const TargetRegisterClass *
Z80RegisterInfo::getLargestLegalSuperClass(const TargetRegisterClass *RC,
                                           const MachineFunction &) const {
  if (Z80::GR16RegClass.hasSubClassEq(RC))
    return &Z80::GR16RegClass;
  if (Z80::GR8RegClass.hasSubClassEq(RC))
    return &Z80::GR8RegClass;
  return RC;
}

// A long value made to live in the one register an instruction wants (A, HL) leaves nothing
// to allocate around it: "ran out of registers". Z80ConstrainedCopies keeps values in the full
// classes; this keeps the coalescer from undoing that. The allocator's hints still give a
// copy the same register at both ends when that is free.
bool Z80RegisterInfo::shouldCoalesce(MachineInstr *, const TargetRegisterClass *,
                                     unsigned, const TargetRegisterClass *,
                                     unsigned, const TargetRegisterClass *NewRC,
                                     LiveIntervals &) const {
  return NewRC == &Z80::GR8RegClass || NewRC == &Z80::GR16RegClass;
}
