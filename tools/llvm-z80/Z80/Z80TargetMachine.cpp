#include "Z80TargetMachine.h"
#include "Z80.h"
#include "TargetInfo/Z80TargetInfo.h"
#include "llvm/CodeGen/Passes.h"
#include "llvm/CodeGen/TargetLoweringObjectFileImpl.h"
#include "llvm/CodeGen/TargetPassConfig.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Support/Compiler.h"

using namespace llvm;

extern "C" LLVM_ABI LLVM_EXTERNAL_VISIBILITY void LLVMInitializeZ80Target() {
  RegisterTargetMachine<Z80TargetMachine> X(getTheZ80Target());
  PassRegistry &PR = *PassRegistry::getPassRegistry();
  initializeZ80AsmPrinterPass(PR);
  initializeZ80ConstrainedCopiesPass(PR);
  initializeZ80PointerBasePass(PR);
  initializeZ80NarrowLoopsPass(PR);
  initializeZ80DAGToDAGISelLegacyPass(PR);
}

Z80TargetMachine::Z80TargetMachine(const Target &T, const Triple &TT,
                                   StringRef CPU, StringRef FS,
                                   const TargetOptions &Options,
                                   std::optional<Reloc::Model> RM,
                                   std::optional<CodeModel::Model> CM,
                                   CodeGenOptLevel OL, bool JIT)
    : CodeGenTargetMachineImpl(
          T, TT.computeDataLayout(), TT, CPU.empty() ? "z80" : CPU, FS, Options,
          RM.value_or(Reloc::Static), getEffectiveCodeModel(CM, CodeModel::Small),
          OL),
      Subtarget(TT, CPU.empty() ? "z80" : CPU, FS, *this) {
  TLOF = std::make_unique<TargetLoweringObjectFileELF>();
  initAsmInfo();
}

namespace {
class Z80PassConfig : public TargetPassConfig {
public:
  Z80PassConfig(Z80TargetMachine &TM, PassManagerBase &PM)
      : TargetPassConfig(TM, PM) {}

  void addIRPasses() override {
    if (getOptLevel() != CodeGenOptLevel::None)
      addPass(createZ80NarrowLoops());
    TargetPassConfig::addIRPasses();
  }

  bool addInstSelector() override {
    addPass(createZ80ISelDag(getTM<Z80TargetMachine>(), getOptLevel()));
    return false;
  }

  void addPreRegAlloc() override {
    addPass(createZ80PointerBase());
    addPass(createZ80ConstrainedCopies());
  }

  // The fast allocator cannot satisfy instructions that each want HL or A; -O0 is no
  // different a program to allocate.
  void addFastRegAlloc() override { addOptimizedRegAlloc(); }
};
} // namespace

TargetPassConfig *Z80TargetMachine::createPassConfig(PassManagerBase &PM) {
  return new Z80PassConfig(*this, PM);
}
