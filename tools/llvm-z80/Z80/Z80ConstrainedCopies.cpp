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

// A checked operation whose flag only decides the next jump: the operation itself sets the flag
// the jump can test, so nothing materializes it. Copies may sit between them, as `ld` keeps
// the flags; anything that changes them ends the search.
static bool jumpOnFlag(MachineInstr &MI, const TargetInstrInfo &TII, MachineRegisterInfo &MRI,
                       const TargetRegisterInfo &TRI) {
  unsigned Real, Cond;
  switch (MI.getOpcode()) {
  case Z80::SADDO8: Real = Z80::ADD8r; Cond = Z80CC::PE; break;
  case Z80::SSUBO8: Real = Z80::SUB8r; Cond = Z80CC::PE; break;
  case Z80::UADDO8: Real = Z80::ADD8r; Cond = Z80CC::C; break;
  case Z80::USUBO8: Real = Z80::SUB8r; Cond = Z80CC::C; break;
  case Z80::SADDO16: Real = Z80::SADD16; Cond = Z80CC::PE; break;
  case Z80::SSUBO16: Real = Z80::SUB16; Cond = Z80CC::PE; break;
  case Z80::UADDO16: Real = Z80::ADD16; Cond = Z80CC::C; break;
  case Z80::USUBO16: Real = Z80::SUB16; Cond = Z80CC::C; break;
  default:
    return false;
  }
  // The flag through its copies to the one test of it.
  Register Flag = MI.getOperand(1).getReg();
  SmallVector<MachineInstr *, 4> Dead;
  MachineInstr *Test = nullptr;
  while (MRI.hasOneNonDBGUse(Flag)) {
    MachineInstr &User = *MRI.use_instr_nodbg_begin(Flag);
    if (User.isCopy() && User.getOperand(0).getReg().isVirtual() &&
        !User.getOperand(1).getSubReg()) {
      Dead.push_back(&User);
      Flag = User.getOperand(0).getReg();
      continue;
    }
    if (User.getOpcode() == Z80::TESTZ8 ||
        (User.getOpcode() == Z80::CP8i && User.getOperand(1).getImm() == 1))
      Test = &User;
    break;
  }
  if (!Test || Test->getParent() != MI.getParent())
    return false;
  // The jump must come next, with nothing changing the flags before it.
  MachineBasicBlock &MBB = *MI.getParent();
  MachineInstr *Jump = nullptr;
  bool PassedTest = false;
  for (auto I = std::next(MI.getIterator()); I != MBB.end(); ++I) {
    if (&*I == Test) {
      PassedTest = true;
      continue;
    }
    if (I->getOpcode() == Z80::JPcc) {
      Jump = &*I;
      break;
    }
    if (I->modifiesRegister(Z80::F, &TRI) || I->readsRegister(Z80::F, &TRI))
      return false;
  }
  if (!Jump || !PassedTest || Jump->getOperand(1).getImm() > Z80CC::Z)
    return false;
  // `or a` is nonzero when it overflowed, and `cp 1` zero.
  unsigned Overflowed = Test->getOpcode() == Z80::TESTZ8 ? Z80CC::NZ : Z80CC::Z;
  Jump->getOperand(1).setImm(Jump->getOperand(1).getImm() == Overflowed ? Cond : Cond ^ 1);
  Test->eraseFromParent();
  for (MachineInstr *Copy : llvm::reverse(Dead))
    Copy->eraseFromParent();
  BuildMI(MBB, MI, MI.getDebugLoc(), TII.get(Real), MI.getOperand(0).getReg())
      .addReg(MI.getOperand(2).getReg())
      .addReg(MI.getOperand(3).getReg());
  MI.eraseFromParent();
  return true;
}

bool Z80ConstrainedCopies::runOnMachineFunction(MachineFunction &MF) {
  const TargetInstrInfo &TII = *MF.getSubtarget().getInstrInfo();
  MachineRegisterInfo &MRI = MF.getRegInfo();
  const TargetRegisterInfo &TRI = *MF.getSubtarget().getRegisterInfo();
  SmallVector<Register, 32> Freed;

  for (MachineBasicBlock &MBB : MF)
    for (MachineInstr &MI : llvm::make_early_inc_range(MBB))
      jumpOnFlag(MI, TII, MRI, TRI);

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
