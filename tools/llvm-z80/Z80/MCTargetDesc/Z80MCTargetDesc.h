#ifndef LLVM_LIB_TARGET_Z80_MCTARGETDESC_Z80MCTARGETDESC_H
#define LLVM_LIB_TARGET_Z80_MCTARGETDESC_Z80MCTARGETDESC_H

#include <memory>

namespace llvm {
class MCAsmBackend;
class MCCodeEmitter;
class MCContext;
class MCInstrInfo;
class MCObjectTargetWriter;
class MCRegisterInfo;
class MCSubtargetInfo;
class MCTargetOptions;
class Target;

MCCodeEmitter *createZ80MCCodeEmitter(const MCInstrInfo &MCII, MCContext &Ctx);
MCAsmBackend *createZ80AsmBackend(const Target &T, const MCSubtargetInfo &STI,
                                  const MCRegisterInfo &MRI,
                                  const MCTargetOptions &Options);
} // namespace llvm

#define GET_REGINFO_ENUM
#include "Z80GenRegisterInfo.inc"

#define GET_INSTRINFO_ENUM
#include "Z80GenInstrInfo.inc"

#define GET_SUBTARGETINFO_ENUM
#include "Z80GenSubtargetInfo.inc"

#endif
