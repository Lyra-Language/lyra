#include "Z80Subtarget.h"
#include "Z80TargetMachine.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"

#define DEBUG_TYPE "z80-subtarget"

#define GET_SUBTARGETINFO_TARGET_DESC
#define GET_SUBTARGETINFO_CTOR
#include "Z80GenSubtargetInfo.inc"

using namespace llvm;

Z80Subtarget::Z80Subtarget(const Triple &TT, StringRef CPU, StringRef FS,
                           const Z80TargetMachine &TM)
    : Z80GenSubtargetInfo(TT, CPU, /*TuneCPU*/ CPU, FS), InstrInfo(*this),
      TLInfo(TM, *this) {
  ParseSubtargetFeatures(CPU, /*TuneCPU*/ CPU, FS);
}
