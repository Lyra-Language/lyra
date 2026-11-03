#ifndef LLVM_LIB_TARGET_Z80_Z80REGISTERINFO_H
#define LLVM_LIB_TARGET_Z80_Z80REGISTERINFO_H

#include "llvm/CodeGen/TargetRegisterInfo.h"

#define GET_REGINFO_HEADER
#include "Z80GenRegisterInfo.inc"

namespace llvm {
class Z80RegisterInfo : public Z80GenRegisterInfo {
public:
  Z80RegisterInfo();

  const MCPhysReg *getCalleeSavedRegs(const MachineFunction *MF) const override;
  const uint32_t *getCallPreservedMask(const MachineFunction &MF,
                                       CallingConv::ID CC) const override;
  BitVector getReservedRegs(const MachineFunction &MF) const override;
  bool eliminateFrameIndex(MachineBasicBlock::iterator MI, int SPAdj,
                           unsigned FIOperandNum,
                           RegScavenger *RS = nullptr) const override;
  Register getFrameRegister(const MachineFunction &MF) const override;
  const TargetRegisterClass *
  getPointerRegClass(unsigned Kind = 0) const override;
  const TargetRegisterClass *
  getLargestLegalSuperClass(const TargetRegisterClass *RC,
                            const MachineFunction &MF) const override;
  bool shouldCoalesce(MachineInstr *MI, const TargetRegisterClass *SrcRC,
                      unsigned SubReg, const TargetRegisterClass *DstRC,
                      unsigned DstSubReg, const TargetRegisterClass *NewRC,
                      LiveIntervals &LIS) const override;
};
} // namespace llvm

#endif
