#ifndef LLVM_LIB_TARGET_Z80_MCTARGETDESC_Z80INSTPRINTER_H
#define LLVM_LIB_TARGET_Z80_MCTARGETDESC_Z80INSTPRINTER_H

#include "llvm/MC/MCInstPrinter.h"

namespace llvm {
class Z80InstPrinter : public MCInstPrinter {
public:
  using MCInstPrinter::MCInstPrinter;

  void printInst(const MCInst *MI, uint64_t Address, StringRef Annot,
                 const MCSubtargetInfo &STI, raw_ostream &O) override;
  std::pair<const char *, uint64_t>
  getMnemonic(const MCInst &MI) const override;
  void printInstruction(const MCInst *MI, uint64_t Address, raw_ostream &O);
  static const char *getRegisterName(MCRegister Reg);

private:
  void printOperand(const MCInst *MI, unsigned OpNo, raw_ostream &O);
  void printCC(const MCInst *MI, unsigned OpNo, raw_ostream &O);
  void printMemIX(const MCInst *MI, unsigned OpNo, raw_ostream &O);
};
} // namespace llvm

#endif
