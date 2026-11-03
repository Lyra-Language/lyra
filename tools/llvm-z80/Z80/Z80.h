#ifndef LLVM_LIB_TARGET_Z80_Z80_H
#define LLVM_LIB_TARGET_Z80_Z80_H

#include "llvm/Support/CodeGen.h"

namespace llvm {
class FunctionPass;
class PassRegistry;
class Z80TargetMachine;

FunctionPass *createZ80ISelDag(Z80TargetMachine &TM, CodeGenOptLevel OptLevel);
FunctionPass *createZ80ConstrainedCopies();
void initializeZ80ConstrainedCopiesPass(PassRegistry &);
void initializeZ80DAGToDAGISelLegacyPass(PassRegistry &);
void initializeZ80AsmPrinterPass(PassRegistry &);

namespace Z80CC {
// The `cc` field of `jp cc, nn`. A code and its opposite differ in the lowest bit.
enum CondCode { NZ = 0, Z = 1, NC = 2, C = 3, PO = 4, PE = 5, P = 6, M = 7 };
} // namespace Z80CC
} // namespace llvm

#endif
