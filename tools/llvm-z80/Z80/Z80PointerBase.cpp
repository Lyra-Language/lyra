//===-- Z80PointerBase.cpp - fields through IY ----------------------------------===//
//
// A field through a pointer in HL is `ld de, offset; add hl, de; ld r, (hl)`, with the pointer
// lost unless it was copied first and two pairs out of three taken. Through IY it is
// `ld r, (iy+d)`, once IY has the pointer, and the pointer need not stay in a register at all.
//
// IY is not given to the allocator: it would be one register for every pointer, and spilling it
// takes a register there may not be. So it stays reserved and this pass, the last with the
// program in SSA form, sets it: each base-and-displacement access selection marked (LD8ro and
// the like) goes through IY, a plain `(hl)` too if its pointer is used for more than one
// access, and a SETIY goes in wherever IY might not hold the pointer already. SSA is what
// makes that a small problem: a virtual register is one value, so "IY holds %p" stays true
// until IY is written or %p is defined again, by another pass of a loop. A forward walk over
// the blocks finds where it holds on entry.
//
// A call takes IY, as it takes everything but IX, unless it is to a helper written by hand that
// is known to leave it. The interrupt saves it (pkg/rom/z80.go).
//
//===----------------------------------------------------------------------===//

#include "Z80.h"
#include "Z80RegisterInfo.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/PostOrderIterator.h"
#include "llvm/ADT/SmallVector.h"
#include "llvm/ADT/StringSwitch.h"
#include "llvm/CodeGen/MachineFunctionPass.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/MachineRegisterInfo.h"
#include "llvm/CodeGen/TargetInstrInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"

using namespace llvm;

#define PASS_NAME "Z80 pointers through IY"

namespace {
class Z80PointerBase : public MachineFunctionPass {
public:
  static char ID;
  Z80PointerBase() : MachineFunctionPass(ID) {}
  StringRef getPassName() const override { return PASS_NAME; }
  bool runOnMachineFunction(MachineFunction &MF) override;
};

// What IY is known to hold at a point. Top is a block not yet reached by the walk.
struct Held {
  enum Kind { Top, Known, Unknown } What = Top;
  Register Pointer;
  static Held unknown() { return {Unknown, Register()}; }
  static Held known(Register R) { return {Known, R}; }
  bool operator==(const Held &O) const { return What == O.What && Pointer == O.Pointer; }
  Held meet(const Held &O) const {
    if (What == Top)
      return O;
    if (O.What == Top || *this == O)
      return *this;
    return unknown();
  }
};

// An access IY can make, with its pointer's operand.
struct Access {
  unsigned Opcode = 0;  // Its IY form.
  unsigned Pointer = 0; // The operand holding the pointer.
  bool Marked = false;  // Selection gave it a displacement, so it has no other form.
};
} // namespace

char Z80PointerBase::ID = 0;

INITIALIZE_PASS(Z80PointerBase, "z80-pointer-base", PASS_NAME, false, false)

FunctionPass *llvm::createZ80PointerBase() { return new Z80PointerBase(); }

static Access accessOf(const MachineInstr &MI) {
  switch (MI.getOpcode()) {
  case Z80::LD8ro: return {Z80::LD8ry, 1, true};
  case Z80::LD16ro: return {Z80::LD16ry, 1, true};
  case Z80::ST8ro: return {Z80::ST8yr, 0, true};
  case Z80::ST16ro: return {Z80::ST16yr, 0, true};
  case Z80::LD8rp: return {Z80::LD8ry, 1, false};
  case Z80::LD16rp: return {Z80::LD16ry, 1, false};
  case Z80::ST8pr: return {Z80::ST8yr, 0, false};
  case Z80::ST16pr: return {Z80::ST16yr, 0, false};
  default: return {};
  }
}

// The value a pointer operand is a copy of, as a copy holds the same value.
static Register pointerOf(const MachineInstr &MI, const Access &A,
                          const MachineRegisterInfo &MRI) {
  Register R = MI.getOperand(A.Pointer).getReg();
  while (R.isVirtual()) {
    const MachineInstr *Def = MRI.getUniqueVRegDef(R);
    if (!Def || !Def->isCopy() || Def->getOperand(1).getSubReg() ||
        !Def->getOperand(1).getReg().isVirtual())
      break;
    R = Def->getOperand(1).getReg();
  }
  return R;
}

// Helpers in pkg/rom/z80.go, which use no index register.
static bool leavesIY(const MachineOperand &Callee) {
  if (!Callee.isSymbol())
    return false;
  return StringSwitch<bool>(Callee.getSymbolName())
      .Cases({"__mulhi3", "__z80_umul32", "__udivhi3", "__umodhi3"}, true)
      .Cases({"__z80_otir", "__z80_bank"}, true)
      .Default(false);
}

static bool takesIY(const MachineInstr &MI, const TargetRegisterInfo &TRI) {
  if (MI.isInlineAsm())
    return true;
  if (MI.isCall()) {
    return !(MI.getNumOperands() > 0 && leavesIY(MI.getOperand(0)));
  }
  return MI.modifiesRegister(Z80::IY, &TRI);
}

bool Z80PointerBase::runOnMachineFunction(MachineFunction &MF) {
  const TargetInstrInfo &TII = *MF.getSubtarget().getInstrInfo();
  const TargetRegisterInfo &TRI = *MF.getSubtarget().getRegisterInfo();
  MachineRegisterInfo &MRI = MF.getRegInfo();

  // How many accesses each pointer has. One plain `(hl)` is better left: a pointer alone in HL
  // is 7 T-states, and through IY 44.
  DenseMap<Register, unsigned> Uses;
  for (MachineBasicBlock &MBB : MF) {
    for (MachineInstr &MI : MBB) {
      Access A = accessOf(MI);
      if (!A.Opcode || MI.getOperand(A.Pointer).getSubReg())
        continue;
      Register P = pointerOf(MI, A, MRI);
      if (P.isVirtual())
        ++Uses[P];
    }
  }
  auto Taken = [&](const MachineInstr &MI, const Access &A, Register &P) {
    if (!A.Opcode || MI.getOperand(A.Pointer).getSubReg())
      return false;
    P = pointerOf(MI, A, MRI);
    return P.isVirtual() && (A.Marked || Uses.lookup(P) > 1);
  };
  auto Step = [&](const MachineInstr &MI, Held &H) {
    Access A = accessOf(MI);
    Register P;
    if (Taken(MI, A, P)) {
      H = Held::known(P);
      return;
    }
    if (takesIY(MI, TRI)) {
      H = Held::unknown();
      return;
    }
    for (const MachineOperand &MO : MI.all_defs())
      if (H.What == Held::Known && MO.getReg() == H.Pointer)
        H = Held::unknown();
  };

  // What IY holds entering each block, to a fixed point.
  DenseMap<const MachineBasicBlock *, Held> In, Out;
  ReversePostOrderTraversal<MachineFunction *> Order(&MF);
  In[&MF.front()] = Held::unknown();
  for (bool Changed = true; Changed;) {
    Changed = false;
    for (MachineBasicBlock *MBB : Order) {
      Held H = MBB == &MF.front() ? Held::unknown() : Held();
      for (MachineBasicBlock *Pred : MBB->predecessors())
        H = H.meet(Out.lookup(Pred));
      In[MBB] = H;
      for (const MachineInstr &MI : *MBB)
        Step(MI, H);
      if (!(Out.lookup(MBB) == H)) {
        Out[MBB] = H;
        Changed = true;
      }
    }
  }

  bool Changed = false;
  SmallVector<Register, 16> Copies;
  for (MachineBasicBlock &MBB : MF) {
    Held H = In.lookup(&MBB);
    for (MachineInstr &MI : llvm::make_early_inc_range(MBB)) {
      Access A = accessOf(MI);
      Register P;
      if (!Taken(MI, A, P)) {
        // A marked access with a sub-register's pointer has no other form, and selection makes none.
        assert(!A.Marked && "a displacement with no way through IY");
        Step(MI, H);
        continue;
      }
      DebugLoc DL = MI.getDebugLoc();
      if (!(H == Held::known(P))) {
        BuildMI(MBB, MI, DL, TII.get(Z80::SETIY)).addReg(P);
        // The pointer now goes on in any pair, whatever its accesses wanted.
        if (Z80::GR16RegClass.hasSubClass(MRI.getRegClass(P)))
          MRI.setRegClass(P, &Z80::GR16RegClass);
      }
      H = Held::known(P);
      Register Written = MI.getOperand(A.Pointer).getReg();
      if (Written != P)
        Copies.push_back(Written);
      int64_t Disp = A.Marked ? MI.getOperand(A.Pointer + 1).getImm() : 0;
      auto New = BuildMI(MBB, MI, DL, TII.get(A.Opcode));
      if (A.Pointer == 1)
        New.addDef(MI.getOperand(0).getReg());
      New.addReg(Z80::IY).addImm(Disp);
      if (A.Pointer == 0)
        New.addReg(MI.getOperand(A.Marked ? 2 : 1).getReg());
      New.cloneMemRefs(MI);
      MI.eraseFromParent();
      Changed = true;
    }
  }
  // Copies only a pointer's accesses used, and the copies they were of.
  while (!Copies.empty()) {
    Register R = Copies.pop_back_val();
    if (!R.isVirtual() || !MRI.use_nodbg_empty(R))
      continue;
    MachineInstr *Def = MRI.getUniqueVRegDef(R);
    if (!Def || !Def->isCopy())
      continue;
    Copies.push_back(Def->getOperand(1).getReg());
    Def->eraseFromParent();
  }
  return Changed;
}
