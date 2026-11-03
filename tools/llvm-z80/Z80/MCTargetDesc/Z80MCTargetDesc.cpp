#include "Z80MCTargetDesc.h"
#include "Z80InstPrinter.h"
#include "TargetInfo/Z80TargetInfo.h"
#include "llvm/MC/MCAsmInfoELF.h"
#include "llvm/MC/MCCodeEmitter.h"
#include "llvm/MC/MCInstrInfo.h"
#include "llvm/MC/MCRegisterInfo.h"
#include "llvm/MC/MCSubtargetInfo.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Support/Compiler.h"

#define GET_INSTRINFO_MC_DESC
#include "Z80GenInstrInfo.inc"

#define GET_SUBTARGETINFO_MC_DESC
#include "Z80GenSubtargetInfo.inc"

#define GET_REGINFO_MC_DESC
#include "Z80GenRegisterInfo.inc"

using namespace llvm;

namespace {
class Z80MCAsmInfo : public MCAsmInfoELF {
public:
  Z80MCAsmInfo(const Triple &, const MCTargetOptions &Options) : MCAsmInfoELF(Options) {
    CodePointerSize = 2;
    CalleeSaveStackSlotSize = 2;
    CommentString = ";";
    SupportsDebugInformation = false;
    IsLittleEndian = true;
  }
};
} // namespace

static MCInstrInfo *createZ80MCInstrInfo() {
  MCInstrInfo *X = new MCInstrInfo();
  InitZ80MCInstrInfo(X);
  return X;
}

static MCRegisterInfo *createZ80MCRegisterInfo(const Triple &) {
  MCRegisterInfo *X = new MCRegisterInfo();
  InitZ80MCRegisterInfo(X, 0);
  return X;
}

static MCSubtargetInfo *createZ80MCSubtargetInfo(const Triple &TT, StringRef CPU,
                                                 StringRef FS) {
  return createZ80MCSubtargetInfoImpl(TT, CPU, /*TuneCPU*/ CPU, FS);
}

static MCInstPrinter *createZ80MCInstPrinter(const Triple &, unsigned,
                                             const MCAsmInfo &MAI, const MCInstrInfo &MII,
                                             const MCRegisterInfo &MRI) {
  return new Z80InstPrinter(MAI, MII, MRI);
}

extern "C" LLVM_ABI LLVM_EXTERNAL_VISIBILITY void LLVMInitializeZ80TargetMC() {
  RegisterMCAsmInfo<Z80MCAsmInfo> X(getTheZ80Target());
  TargetRegistry::RegisterMCInstrInfo(getTheZ80Target(), createZ80MCInstrInfo);
  TargetRegistry::RegisterMCRegInfo(getTheZ80Target(), createZ80MCRegisterInfo);
  TargetRegistry::RegisterMCSubtargetInfo(getTheZ80Target(), createZ80MCSubtargetInfo);
  TargetRegistry::RegisterMCInstPrinter(getTheZ80Target(), createZ80MCInstPrinter);
  TargetRegistry::RegisterMCCodeEmitter(getTheZ80Target(), createZ80MCCodeEmitter);
  TargetRegistry::RegisterMCAsmBackend(getTheZ80Target(), createZ80AsmBackend);
}
