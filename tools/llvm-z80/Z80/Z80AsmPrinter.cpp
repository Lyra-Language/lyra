#include "Z80.h"
#include "TargetInfo/Z80TargetInfo.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/CodeGen/AsmPrinter.h"
#include "llvm/CodeGen/MachineInstr.h"
#include "llvm/IR/Constants.h"
#include "llvm/MC/MCContext.h"
#include "llvm/MC/MCExpr.h"
#include "llvm/MC/MCInst.h"
#include "llvm/MC/MCStreamer.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Support/Compiler.h"

using namespace llvm;

namespace {
class Z80AsmPrinter : public AsmPrinter {
public:
  static char ID;
  Z80AsmPrinter(TargetMachine &TM, std::unique_ptr<MCStreamer> Streamer)
      : AsmPrinter(TM, std::move(Streamer), ID) {}

  StringRef getPassName() const override { return "Z80 Assembly Printer"; }

  void emitInstruction(const MachineInstr *MI) override {
    MCInst Out;
    Out.setOpcode(MI->getOpcode());
    for (const MachineOperand &MO : MI->operands()) {
      MCOperand Op;
      if (!lowerOperand(MO, Op))
        continue;
      Out.addOperand(Op);
    }
    EmitToStreamer(*OutStreamer, Out);
  }

private:
  MCOperand symbol(const MCSymbol *Sym, int64_t Offset) {
    MCContext &Ctx = OutContext;
    const MCExpr *Expr = MCSymbolRefExpr::create(Sym, Ctx);
    if (Offset)
      Expr = MCBinaryExpr::createAdd(Expr, MCConstantExpr::create(Offset, Ctx), Ctx);
    return MCOperand::createExpr(Expr);
  }

  bool lowerOperand(const MachineOperand &MO, MCOperand &Op) {
    switch (MO.getType()) {
    case MachineOperand::MO_Register:
      if (MO.isImplicit())
        return false;
      Op = MCOperand::createReg(MO.getReg());
      return true;
    case MachineOperand::MO_Immediate:
      Op = MCOperand::createImm(MO.getImm());
      return true;
    case MachineOperand::MO_MachineBasicBlock:
      Op = symbol(MO.getMBB()->getSymbol(), 0);
      return true;
    case MachineOperand::MO_GlobalAddress:
      Op = symbol(getSymbol(MO.getGlobal()), MO.getOffset());
      return true;
    case MachineOperand::MO_ExternalSymbol:
      Op = symbol(GetExternalSymbolSymbol(MO.getSymbolName()), MO.getOffset());
      return true;
    case MachineOperand::MO_BlockAddress:
      Op = symbol(GetBlockAddressSymbol(MO.getBlockAddress()), MO.getOffset());
      return true;
    case MachineOperand::MO_RegisterMask:
      return false;
    default:
      llvm_unreachable("an operand the Z80 printer does not know");
    }
  }
};
} // namespace

char Z80AsmPrinter::ID = 0;

INITIALIZE_PASS(Z80AsmPrinter, "z80-asm-printer", "Z80 Assembly Printer", false, false)

extern "C" LLVM_ABI LLVM_EXTERNAL_VISIBILITY void LLVMInitializeZ80AsmPrinter() {
  RegisterAsmPrinter<Z80AsmPrinter> X(getTheZ80Target());
}
