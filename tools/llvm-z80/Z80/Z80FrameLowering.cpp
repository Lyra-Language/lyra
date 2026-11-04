//===-- Z80FrameLowering.cpp - the Z80's stack frames ---------------------------===//
//
// From the top: stack arguments, the return address, the caller's IX, this function's locals
// and spills, and at the bottom the arguments of the calls it makes. SP does not move in the
// body and IX holds the same value, so every slot is (ix+d) and no register is needed to
// allocate the frame: the arguments in HL and DE, and a result in HL, are left alone.
//
//===----------------------------------------------------------------------===//

#include "Z80FrameLowering.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/CodeGen/MachineFrameInfo.h"
#include "llvm/CodeGen/MachineFunction.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/TargetInstrInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"

using namespace llvm;

// The local area starts below the saved IX.
Z80FrameLowering::Z80FrameLowering()
    : TargetFrameLowering(StackGrowsDown, Align(1), -2) {}

// A function with no slot and no stack argument has no use for IX.
static bool needsFrame(const MachineFunction &MF) {
  const MachineFrameInfo &MFI = MF.getFrameInfo();
  return MFI.getStackSize() > 0 || MFI.getNumFixedObjects() > 0;
}

// How far above SP to keep IX. A displacement reaches -128 to 127, so at SP itself a frame's upper
// half would be out of reach, and each access there costs about 340 T-states. A larger frame has IX
// in its middle instead. The one thing that wants IX to be SP is a far outgoing argument, which reads
// it as a value (LowerCall), and it would have to be a call with over 126 bytes of arguments.
static int64_t biasOf(const MachineFunction &MF) {
  const MachineFrameInfo &MFI = MF.getFrameInfo();
  int64_t Top = MFI.getStackSize() + 2;
  for (int FI = MFI.getObjectIndexBegin(); FI < 0; ++FI)
    Top = std::max(Top, int64_t(MFI.getStackSize()) + 2 + MFI.getObjectOffset(FI) +
                            MFI.getObjectSize(FI));
  if (Top <= 127)
    return 0;
  for (const MachineBasicBlock &MBB : MF)
    for (const MachineInstr &MI : MBB)
      if (MI.isCopy() && MI.getOperand(1).getReg() == Z80::IX)
        return 0;
  return 128;
}

// The outgoing arguments were selected as IX plus an offset from SP.
static void rebaseArguments(MachineFunction &MF, int64_t Bias) {
  for (MachineBasicBlock &MBB : MF) {
    for (MachineInstr &MI : MBB) {
      unsigned Base;
      switch (MI.getOpcode()) {
      case Z80::LD8rf:
      case Z80::LD16rf:
        Base = 1;
        break;
      case Z80::ST8fr:
      case Z80::ST16fr:
        Base = 0;
        break;
      default:
        continue;
      }
      if (MI.getOperand(Base).isReg())
        MI.getOperand(Base + 1).setImm(MI.getOperand(Base + 1).getImm() - Bias);
    }
  }
}

void Z80FrameLowering::emitPrologue(MachineFunction &MF,
                                    MachineBasicBlock &MBB) const {
  if (!needsFrame(MF))
    return;
  const TargetInstrInfo &TII = *MF.getSubtarget().getInstrInfo();
  uint64_t Size = MF.getFrameInfo().getStackSize();
  if (Size > 0x7FFF)
    report_fatal_error("Z80: a frame is larger than the address space allows");
  MachineBasicBlock::iterator I = MBB.begin();
  DebugLoc DL;
  BuildMI(MBB, I, DL, TII.get(Z80::PUSHIX));
  BuildMI(MBB, I, DL, TII.get(Z80::LDIXi)).addImm(-int64_t(Size) & 0xFFFF);
  BuildMI(MBB, I, DL, TII.get(Z80::ADDIXSP));
  if (Size > 0)
    BuildMI(MBB, I, DL, TII.get(Z80::LDSPIX));
  int64_t Bias = biasOf(MF);
  MF.getFrameInfo().setOffsetAdjustment(Bias);
  if (Bias) {
    BuildMI(MBB, I, DL, TII.get(Z80::LDIXi)).addImm(Bias);
    BuildMI(MBB, I, DL, TII.get(Z80::ADDIXSP));
    rebaseArguments(MF, Bias);
  }
}

void Z80FrameLowering::emitEpilogue(MachineFunction &MF,
                                    MachineBasicBlock &MBB) const {
  if (!needsFrame(MF))
    return;
  const TargetInstrInfo &TII = *MF.getSubtarget().getInstrInfo();
  uint64_t Size = MF.getFrameInfo().getStackSize();
  MachineBasicBlock::iterator I = MBB.getFirstTerminator();
  DebugLoc DL = I != MBB.end() ? I->getDebugLoc() : DebugLoc();
  if (Size > 0) {
    BuildMI(MBB, I, DL, TII.get(Z80::LDIXi)).addImm(Size);
    BuildMI(MBB, I, DL, TII.get(Z80::ADDIXSP));
    BuildMI(MBB, I, DL, TII.get(Z80::LDSPIX));
  }
  BuildMI(MBB, I, DL, TII.get(Z80::POPIX));
}

bool Z80FrameLowering::hasReservedCallFrame(const MachineFunction &) const {
  return true;
}

MachineBasicBlock::iterator Z80FrameLowering::eliminateCallFramePseudoInstr(
    MachineFunction &, MachineBasicBlock &MBB,
    MachineBasicBlock::iterator I) const {
  return MBB.erase(I);
}

bool Z80FrameLowering::hasFPImpl(const MachineFunction &) const { return true; }
