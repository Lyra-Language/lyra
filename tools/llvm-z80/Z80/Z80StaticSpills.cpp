//===-- Z80StaticSpills.cpp - spill slots at fixed addresses --------------------===//
//
// A spill through IX is 38 T-states a pair each way; `ld (nn), hl` is 16 and `ld (nn), de` 20, and
// half of what a game's frame does is spills. A fixed address is only right for a function
// that cannot be running twice at once, as a C compiler for the Z80 can only be told. A game is
// one module, though, with `main` and its interrupt handlers the only ways in, so here it can
// be proved from the call graph. A function qualifies if it
//
//   - is not on a cycle of calls, an indirect call counting as a call of everything whose
//     address is taken;
//   - has its address never taken, and is reached by only one way in. An interrupt can come
//     while `main` is anywhere, so what both reach could be running twice;
//   - and is itself only entered by those ways: `main`, a handler, or something only they call.
//
// Calls the backend makes up (helpers for multiplying, memset) are compiled apart, with nothing
// proved, and call nothing back.
//
// Only pair-sized spill slots move: a byte has no such instruction but A's, and a slot whose
// address is taken has to be in memory the frame can point at anyway. A function left with no
// slots needs no frame, which is about 100 T-states a call more.
//
//===----------------------------------------------------------------------===//

#include "Z80.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/SmallVector.h"
#include "llvm/CodeGen/MachineFrameInfo.h"
#include "llvm/CodeGen/MachineFunctionPass.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/TargetInstrInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/DerivedTypes.h"
#include "llvm/IR/GlobalVariable.h"
#include "llvm/IR/InstIterator.h"
#include "llvm/IR/InstrTypes.h"
#include "llvm/IR/Module.h"
#include "llvm/Support/CommandLine.h"

using namespace llvm;

#define PASS_NAME "Z80 spill slots at fixed addresses"

// For tests, whose functions are all public: each is a way in, called only from the outside.
static cl::opt<bool> PublicAreRoots(
    "z80-public-functions-are-entries", cl::Hidden, cl::init(false),
    cl::desc("Treat every public function as a way into the program"));

namespace {
class Z80StaticSpills : public MachineFunctionPass {
public:
  static char ID;
  Z80StaticSpills() : MachineFunctionPass(ID) {}
  StringRef getPassName() const override { return PASS_NAME; }
  bool doInitialization(Module &M) override;
  bool runOnMachineFunction(MachineFunction &MF) override;

private:
  DenseSet<const Function *> OnlyOnce;
};
} // namespace

char Z80StaticSpills::ID = 0;

INITIALIZE_PASS(Z80StaticSpills, "z80-static-spills", PASS_NAME, false, false)

FunctionPass *llvm::createZ80StaticSpills() { return new Z80StaticSpills(); }

static bool isWayIn(const Function &F) {
  if (F.isDeclaration() || F.hasLocalLinkage())
    return false;
  return PublicAreRoots || F.getName() == "main" || F.getName().starts_with("__lyra_interrupt_");
}

bool Z80StaticSpills::doInitialization(Module &M) {
  OnlyOnce.clear();
  // Who each function may call.
  SmallVector<const Function *, 16> AddressTaken;
  for (const Function &F : M)
    if (F.hasAddressTaken())
      AddressTaken.push_back(&F);
  DenseMap<const Function *, SmallVector<const Function *, 4>> Calls;
  DenseMap<const Function *, unsigned> CalledBy;
  for (const Function &F : M) {
    auto &Out = Calls[&F];
    for (const Instruction &I : instructions(F)) {
      auto *Call = dyn_cast<CallBase>(&I);
      if (!Call)
        continue;
      if (const Function *Callee = Call->getCalledFunction()) {
        if (!Callee->isIntrinsic()) {
          Out.push_back(Callee);
          ++CalledBy[Callee];
        }
      } else {
        for (const Function *T : AddressTaken) {
          Out.push_back(T);
          ++CalledBy[T];
        }
      }
    }
  }
  auto Reach = [&](const Function *From, DenseSet<const Function *> &Seen) {
    const auto &First = Calls[From];
    SmallVector<const Function *, 16> Work(First.begin(), First.end());
    while (!Work.empty()) {
      const Function *F = Work.pop_back_val();
      if (!Seen.insert(F).second)
        continue;
      const auto &Next = Calls[F];
      Work.append(Next.begin(), Next.end());
    }
  };
  // How many ways in reach each function.
  DenseMap<const Function *, unsigned> Ways;
  for (const Function &F : M) {
    if (!isWayIn(F))
      continue;
    DenseSet<const Function *> Seen;
    Reach(&F, Seen);
    Seen.insert(&F);
    for (const Function *R : Seen)
      ++Ways[R];
  }
  for (const Function &F : M) {
    if (F.isDeclaration() || F.hasAddressTaken() || Ways.lookup(&F) != 1)
      continue;
    // A way in that something also calls could be running when it is entered again.
    if (!F.hasLocalLinkage() && !isWayIn(F))
      continue;
    if (isWayIn(F) && CalledBy.lookup(&F))
      continue;
    DenseSet<const Function *> Seen;
    Reach(&F, Seen);
    if (!Seen.count(&F))
      OnlyOnce.insert(&F);
  }
  return false;
}

bool Z80StaticSpills::runOnMachineFunction(MachineFunction &MF) {
  if (!OnlyOnce.count(&MF.getFunction()))
    return false;
  MachineFrameInfo &MFI = MF.getFrameInfo();
  // The slots every use of which is a whole pair's spill or reload.
  DenseMap<int, SmallVector<MachineInstr *, 4>> Uses;
  DenseSet<int> Kept;
  for (MachineBasicBlock &MBB : MF) {
    for (MachineInstr &MI : MBB) {
      for (const MachineOperand &MO : MI.operands()) {
        if (!MO.isFI())
          continue;
        bool Pair = MI.getOpcode() == Z80::LD16rf || MI.getOpcode() == Z80::ST16fr;
        if (Pair && MFI.isSpillSlotObjectIndex(MO.getIndex()))
          Uses[MO.getIndex()].push_back(&MI);
        else
          Kept.insert(MO.getIndex());
      }
    }
  }
  for (int FI : Kept)
    Uses.erase(FI);
  if (Uses.empty())
    return false;

  // A table of them for this function, in the order they come, so a build is the same each time.
  SmallVector<int, 16> Slots;
  for (int FI = MFI.getObjectIndexBegin(); FI != MFI.getObjectIndexEnd(); ++FI)
    if (Uses.count(FI))
      Slots.push_back(FI);
  Module &M = *MF.getFunction().getParent();
  auto *Ty = ArrayType::get(Type::getInt8Ty(M.getContext()), Slots.size() * 2);
  auto *Table = new GlobalVariable(M, Ty, false, GlobalValue::InternalLinkage,
                                   ConstantAggregateZero::get(Ty),
                                   "z80.spills." + MF.getName());
  const TargetInstrInfo &TII = *MF.getSubtarget().getInstrInfo();
  int64_t Offset = 0;
  for (int FI : Slots) {
    for (MachineInstr *MI : Uses[FI]) {
      MachineBasicBlock &MBB = *MI->getParent();
      const DebugLoc &DL = MI->getDebugLoc();
      if (MI->getOpcode() == Z80::LD16rf) {
        Register D = MI->getOperand(0).getReg();
        BuildMI(MBB, *MI, DL, TII.get(D == Z80::HL ? Z80::LD16ra : Z80::LD16rm), D)
            .addGlobalAddress(Table, Offset);
      } else {
        Register S = MI->getOperand(2).getReg();
        bool Kill = MI->getOperand(2).isKill();
        BuildMI(MBB, *MI, DL, TII.get(S == Z80::HL ? Z80::ST16ar : Z80::ST16mr))
            .addGlobalAddress(Table, Offset)
            .addReg(S, getKillRegState(Kill));
      }
      MI->eraseFromParent();
    }
    MFI.RemoveStackObject(FI);
    Offset += 2;
  }
  return true;
}
