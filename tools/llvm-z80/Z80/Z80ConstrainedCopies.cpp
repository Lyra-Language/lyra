//===-- Z80ConstrainedCopies.cpp - values stay out of A and HL ------------------===//
//
// Much of the Z80 works only through A or HL. Selection constrains a value's whole virtual
// register to the class its instruction wants, so `cp` of a value an `add` just made asks for
// two different values in A at once, and a long-lived value pins HL: "ran out of registers",
// with nothing left to split. This gives each such operand a register of its own, copied at
// the instruction, and hands the value back the full class. The allocator's hints and the
// coalescer remove the copies that are not needed.
//
//===----------------------------------------------------------------------===//

#include "Z80.h"
#include "Z80RegisterInfo.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/ADT/SmallVector.h"
#include "llvm/CodeGen/MachineFunctionPass.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/MachineRegisterInfo.h"
#include "llvm/CodeGen/TargetInstrInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"

using namespace llvm;

#define PASS_NAME "Z80 constrained operand copies"

namespace {
class Z80ConstrainedCopies : public MachineFunctionPass {
public:
  static char ID;
  Z80ConstrainedCopies() : MachineFunctionPass(ID) {}
  StringRef getPassName() const override { return PASS_NAME; }
  bool runOnMachineFunction(MachineFunction &MF) override;
};
} // namespace

char Z80ConstrainedCopies::ID = 0;

INITIALIZE_PASS(Z80ConstrainedCopies, "z80-constrained-copies", PASS_NAME, false, false)

FunctionPass *llvm::createZ80ConstrainedCopies() { return new Z80ConstrainedCopies(); }

static const TargetRegisterClass *fullClass(const TargetRegisterClass *RC) {
  if (Z80::GR8RegClass.hasSubClassEq(RC))
    return &Z80::GR8RegClass;
  if (Z80::GR16RegClass.hasSubClassEq(RC))
    return &Z80::GR16RegClass;
  return nullptr;
}

bool Z80ConstrainedCopies::runOnMachineFunction(MachineFunction &MF) {
  const TargetInstrInfo &TII = *MF.getSubtarget().getInstrInfo();
  MachineRegisterInfo &MRI = MF.getRegInfo();
  SmallVector<Register, 32> Freed;

  for (MachineBasicBlock &MBB : MF) {
    for (MachineInstr &MI : llvm::make_early_inc_range(MBB)) {
      if (MI.isCopyLike() || MI.isPHI() || MI.isImplicitDef() || MI.isDebugInstr() ||
          MI.isInlineAsm())
        continue;
      const MCInstrDesc &Desc = MI.getDesc();
      DebugLoc DL = MI.getDebugLoc();
      auto After = std::next(MI.getIterator());
      for (unsigned I = 0, E = std::min<unsigned>(Desc.getNumOperands(), MI.getNumOperands());
           I != E; ++I) {
        MachineOperand &MO = MI.getOperand(I);
        if (!MO.isReg() || !MO.getReg().isVirtual())
          continue;
        const TargetRegisterClass *RC = TII.getRegClass(Desc, I);
        const TargetRegisterClass *Full = RC ? fullClass(RC) : nullptr;
        if (!Full || Full == RC)
          continue;
        Register Old = MO.getReg();
        Register New = MRI.createVirtualRegister(RC);
        if (MO.isDef()) {
          BuildMI(MBB, After, DL, TII.get(TargetOpcode::COPY), Old).addReg(New);
        } else {
          BuildMI(MBB, MI, DL, TII.get(TargetOpcode::COPY), New)
              .addReg(Old, {}, MO.getSubReg());
          MO.setSubReg(0);
          MO.setIsKill(false);
        }
        MO.setReg(New);
        Freed.push_back(Old);
      }
    }
  }
  for (Register R : Freed)
    MRI.setRegClass(R, fullClass(MRI.getRegClass(R)));
  return !Freed.empty();
}
