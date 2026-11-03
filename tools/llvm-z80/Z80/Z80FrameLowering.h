#ifndef LLVM_LIB_TARGET_Z80_Z80FRAMELOWERING_H
#define LLVM_LIB_TARGET_Z80_Z80FRAMELOWERING_H

#include "llvm/CodeGen/TargetFrameLowering.h"

namespace llvm {
class Z80FrameLowering : public TargetFrameLowering {
public:
  Z80FrameLowering();

  void emitPrologue(MachineFunction &MF, MachineBasicBlock &MBB) const override;
  void emitEpilogue(MachineFunction &MF, MachineBasicBlock &MBB) const override;
  bool hasReservedCallFrame(const MachineFunction &MF) const override;
  MachineBasicBlock::iterator
  eliminateCallFramePseudoInstr(MachineFunction &MF, MachineBasicBlock &MBB,
                                MachineBasicBlock::iterator I) const override;

protected:
  bool hasFPImpl(const MachineFunction &MF) const override;
};
} // namespace llvm

#endif
