#include "TargetInfo/Z80TargetInfo.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Support/Compiler.h"

using namespace llvm;

Target &llvm::getTheZ80Target() {
  static Target TheZ80Target;
  return TheZ80Target;
}

extern "C" LLVM_ABI LLVM_EXTERNAL_VISIBILITY void LLVMInitializeZ80TargetInfo() {
  RegisterTarget<Triple::z80> X(getTheZ80Target(), "z80", "Zilog Z80", "Z80");
}
