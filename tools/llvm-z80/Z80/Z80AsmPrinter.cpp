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
    if (MI->getOpcode() == Z80::JPcc && MI->getOperand(1).getImm() >= Z80CC::LT) {
      emitSignedJump(MI);
      return;
    }
    if (MI->getOpcode() == Z80::SETCC8) {
      emitSetCC(MI);
      return;
    }
    if (MI->getOpcode() == Z80::CPEQ16 || MI->getOpcode() == Z80::CPEQ16i) {
      emitEquals(MI);
      return;
    }
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
  void jump(unsigned Cond, const MCOperand &Target) {
    MCInst I;
    I.setOpcode(Z80::JPcc);
    I.addOperand(Target);
    I.addOperand(MCOperand::createImm(Cond));
    EmitToStreamer(*OutStreamer, I);
  }

  void plainJump(const MCOperand &Target) {
    MCInst I;
    I.setOpcode(Z80::JP);
    I.addOperand(Target);
    EmitToStreamer(*OutStreamer, I);
  }

  // Less is S and V differing: with overflow, positive means less. GE is the same jumps with
  // their sense turned over.
  void emitSignedJump(const MachineInstr *MI) {
    bool Less = MI->getOperand(1).getImm() == Z80CC::LT;
    MCOperand Target = symbol(MI->getOperand(0).getMBB()->getSymbol(), 0);
    MCSymbol *Overflowed = OutContext.createTempSymbol();
    MCSymbol *Past = OutContext.createTempSymbol();
    jump(Z80CC::PE, symbol(Overflowed, 0));
    jump(Less ? Z80CC::M : Z80CC::P, Target);
    plainJump(symbol(Past, 0));
    OutStreamer->emitLabel(Overflowed);
    jump(Less ? Z80CC::P : Z80CC::M, Target);
    OutStreamer->emitLabel(Past);
  }

  // Z of the whole: the low bytes, and if those match the high.
  void emitEquals(const MachineInstr *MI) {
    const TargetRegisterInfo &TRI = *MI->getMF()->getSubtarget().getRegisterInfo();
    Register X = MI->getOperand(0).getReg();
    MCSymbol *Past = OutContext.createTempSymbol();
    auto Compare = [&](unsigned Sub, int Shift) {
      MCInst Load;
      Load.setOpcode(Z80::LD8rr);
      Load.addOperand(MCOperand::createReg(Z80::A));
      Load.addOperand(MCOperand::createReg(TRI.getSubReg(X, Sub)));
      EmitToStreamer(*OutStreamer, Load);
      MCInst Cp;
      if (MI->getOpcode() == Z80::CPEQ16) {
        Cp.setOpcode(Z80::CP8r);
        Cp.addOperand(MCOperand::createReg(Z80::A));
        Cp.addOperand(MCOperand::createReg(TRI.getSubReg(MI->getOperand(1).getReg(), Sub)));
      } else {
        Cp.setOpcode(Z80::CP8i);
        Cp.addOperand(MCOperand::createReg(Z80::A));
        Cp.addOperand(MCOperand::createImm((MI->getOperand(1).getImm() >> Shift) & 0xFF));
      }
      EmitToStreamer(*OutStreamer, Cp);
    };
    Compare(Z80::sub_lo, 0);
    jump(Z80CC::NZ, symbol(Past, 0));
    Compare(Z80::sub_hi, 8);
    OutStreamer->emitLabel(Past);
  }

  void emitSetCC(const MachineInstr *MI) {
    MCRegister D = MI->getOperand(0).getReg();
    MCSymbol *Past = OutContext.createTempSymbol();
    MCInst Zero;
    Zero.setOpcode(Z80::LD8ri);
    Zero.addOperand(MCOperand::createReg(D));
    Zero.addOperand(MCOperand::createImm(0));
    EmitToStreamer(*OutStreamer, Zero);
    jump(MI->getOperand(1).getImm() ^ 1, symbol(Past, 0));
    MCInst One;
    One.setOpcode(Z80::INC8);
    One.addOperand(MCOperand::createReg(D));
    One.addOperand(MCOperand::createReg(D));
    EmitToStreamer(*OutStreamer, One);
    OutStreamer->emitLabel(Past);
  }

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
