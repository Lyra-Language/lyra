#ifndef LLVM_LIB_TARGET_Z80_Z80SUBTARGET_H
#define LLVM_LIB_TARGET_Z80_Z80SUBTARGET_H

#include "Z80FrameLowering.h"
#include "Z80ISelLowering.h"
#include "Z80InstrInfo.h"
#include "Z80SelectionDAGInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"

#define GET_SUBTARGETINFO_HEADER
#include "Z80GenSubtargetInfo.inc"

namespace llvm {
class Z80TargetMachine;

class Z80Subtarget : public Z80GenSubtargetInfo {
public:
  Z80Subtarget(const Triple &TT, StringRef CPU, StringRef FS,
               const Z80TargetMachine &TM);

  const Z80InstrInfo *getInstrInfo() const override { return &InstrInfo; }
  const TargetFrameLowering *getFrameLowering() const override {
    return &FrameLowering;
  }
  const Z80TargetLowering *getTargetLowering() const override { return &TLInfo; }
  const Z80SelectionDAGInfo *getSelectionDAGInfo() const override {
    return &TSInfo;
  }
  const Z80RegisterInfo *getRegisterInfo() const override {
    return &InstrInfo.getRegisterInfo();
  }

  void ParseSubtargetFeatures(StringRef CPU, StringRef TuneCPU, StringRef FS);

private:
  Z80InstrInfo InstrInfo;
  Z80FrameLowering FrameLowering;
  Z80TargetLowering TLInfo;
  Z80SelectionDAGInfo TSInfo;
};
} // namespace llvm

#endif
