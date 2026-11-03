#include "Z80InstPrinter.h"
#include "llvm/MC/MCExpr.h"
#include "llvm/MC/MCInst.h"
#include "llvm/MC/MCAsmInfo.h"
#include "llvm/MC/MCRegister.h"
#include "llvm/Support/raw_ostream.h"

using namespace llvm;

#include "Z80GenAsmWriter.inc"

void Z80InstPrinter::printInst(const MCInst *MI, uint64_t Address, StringRef Annot,
                               const MCSubtargetInfo &, raw_ostream &O) {
  printInstruction(MI, Address, O);
  printAnnotation(O, Annot);
}

void Z80InstPrinter::printOperand(const MCInst *MI, unsigned OpNo, raw_ostream &O) {
  const MCOperand &Op = MI->getOperand(OpNo);
  if (Op.isReg())
    O << getRegisterName(Op.getReg());
  else if (Op.isImm())
    O << Op.getImm();
  else
    MAI.printExpr(O, *Op.getExpr());
}

void Z80InstPrinter::printCC(const MCInst *MI, unsigned OpNo, raw_ostream &O) {
  static const char *const Names[] = {"nz", "z", "nc", "c", "po", "pe", "p", "m"};
  O << Names[MI->getOperand(OpNo).getImm() & 7];
}

void Z80InstPrinter::printMemIX(const MCInst *MI, unsigned OpNo, raw_ostream &O) {
  int64_t Disp = MI->getOperand(OpNo + 1).getImm();
  O << "(ix" << (Disp < 0 ? "" : "+") << Disp << ")";
}

void Z80InstPrinter::printMemIY(const MCInst *MI, unsigned OpNo, raw_ostream &O) {
  int64_t Disp = MI->getOperand(OpNo + 1).getImm();
  O << "(iy" << (Disp < 0 ? "" : "+") << Disp << ")";
}
