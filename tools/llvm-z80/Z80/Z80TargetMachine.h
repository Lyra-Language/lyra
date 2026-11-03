#ifndef LLVM_LIB_TARGET_Z80_Z80TARGETMACHINE_H
#define LLVM_LIB_TARGET_Z80_Z80TARGETMACHINE_H

#include "Z80Subtarget.h"
#include "llvm/CodeGen/CodeGenTargetMachineImpl.h"
#include "llvm/Target/TargetLoweringObjectFile.h"
#include <memory>
#include <optional>

namespace llvm {
class Z80TargetMachine : public CodeGenTargetMachineImpl {
  std::unique_ptr<TargetLoweringObjectFile> TLOF;
  Z80Subtarget Subtarget;

public:
  Z80TargetMachine(const Target &T, const Triple &TT, StringRef CPU,
                   StringRef FS, const TargetOptions &Options,
                   std::optional<Reloc::Model> RM,
                   std::optional<CodeModel::Model> CM, CodeGenOptLevel OL,
                   bool JIT);

  const Z80Subtarget *getSubtargetImpl(const Function &) const override {
    return &Subtarget;
  }
  TargetPassConfig *createPassConfig(PassManagerBase &PM) override;
  TargetLoweringObjectFile *getObjFileLowering() const override {
    return TLOF.get();
  }
};
} // namespace llvm

#endif
