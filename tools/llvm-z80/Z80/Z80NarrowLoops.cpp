//===-- Z80NarrowLoops.cpp - loop counters in 16 bits ---------------------------===//
//
// Lyra's `for` counts in i64, whatever it counts, and LLVM leaves such a counter alone when its
// uses are only truncated: each pass of the loop then adds and compares eight bytes, on a CPU
// that does sixteen bits at a time. A counter ScalarEvolution can bound is rewritten as an i16,
// and what still wants it wide gets it zero-extended. It is non-negative and fits, so the
// extended value is the same value.
//
//===----------------------------------------------------------------------===//

#include "Z80.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/Instructions.h"
#include "llvm/InitializePasses.h"
#include "llvm/Pass.h"

using namespace llvm;

#define PASS_NAME "Z80 narrow loop counters"

namespace {
class Z80NarrowLoops : public FunctionPass {
public:
  static char ID;
  Z80NarrowLoops() : FunctionPass(ID) {}
  StringRef getPassName() const override { return PASS_NAME; }
  bool runOnFunction(Function &F) override;
  void getAnalysisUsage(AnalysisUsage &AU) const override {
    AU.addRequired<LoopInfoWrapperPass>();
    AU.addRequired<ScalarEvolutionWrapperPass>();
  }
};
} // namespace

char Z80NarrowLoops::ID = 0;

INITIALIZE_PASS_BEGIN(Z80NarrowLoops, "z80-narrow-loops", PASS_NAME, false, false)
INITIALIZE_PASS_DEPENDENCY(LoopInfoWrapperPass)
INITIALIZE_PASS_DEPENDENCY(ScalarEvolutionWrapperPass)
INITIALIZE_PASS_END(Z80NarrowLoops, "z80-narrow-loops", PASS_NAME, false, false)

FunctionPass *llvm::createZ80NarrowLoops() { return new Z80NarrowLoops(); }

// The largest a counter may reach, its increment included, and stay a positive i16.
static const uint64_t Largest = 0x7FFF;

// Rewrites V's uses for its narrow value N: a truncation takes N itself, a comparison with a
// constant that fits compares in 16 bits, and anything else takes N extended.
static void rewriteUses(Value *V, Value *N, Value *Skip) {
  Type *I16 = N->getType();
  for (Use &U : llvm::make_early_inc_range(V->uses())) {
    auto *User = dyn_cast<Instruction>(U.getUser());
    if (!User || User == Skip)
      continue;
    if (auto *T = dyn_cast<TruncInst>(User)) {
      IRBuilder<> B(T);
      unsigned Bits = T->getType()->getIntegerBitWidth();
      if (Bits <= 16) {
        Value *R = Bits == 16 ? N : B.CreateTrunc(N, T->getType());
        T->replaceAllUsesWith(R);
        T->eraseFromParent();
        continue;
      }
    }
    if (auto *C = dyn_cast<ICmpInst>(User)) {
      auto *K = dyn_cast<ConstantInt>(C->getOperand(1));
      if (U.getOperandNo() == 0 && K && K->getValue().isIntN(15) && !K->isNegative()) {
        IRBuilder<> B(C);
        // Both are non-negative in sixteen bits, so a signed predicate means the same.
        Value *R = B.CreateICmp(C->getPredicate(), N, ConstantInt::get(I16, K->getZExtValue()));
        C->replaceAllUsesWith(R);
        C->eraseFromParent();
        continue;
      }
    }
    Instruction *Before = User;
    if (auto *P = dyn_cast<PHINode>(User))
      Before = P->getIncomingBlock(U)->getTerminator();
    IRBuilder<> B(Before);
    U.set(B.CreateZExt(N, V->getType()));
  }
}

static bool narrowLoop(Loop *L, ScalarEvolution &SE) {
  BasicBlock *Header = L->getHeader();
  BasicBlock *Latch = L->getLoopLatch();
  BasicBlock *Preheader = L->getLoopPreheader();
  if (!Latch || !Preheader)
    return false;
  const SCEV *Taken = SE.getConstantMaxBackedgeTakenCount(L);
  auto *Count = dyn_cast<SCEVConstant>(Taken);
  if (!Count || !Count->getValue()->getValue().isIntN(16))
    return false;
  uint64_t Passes = Count->getValue()->getZExtValue();

  bool Changed = false;
  SmallVector<PHINode *, 4> Phis;
  for (PHINode &P : Header->phis())
    if (P.getType()->isIntegerTy() && P.getType()->getIntegerBitWidth() > 16)
      Phis.push_back(&P);
  for (PHINode *P : Phis) {
    auto *Start = dyn_cast<ConstantInt>(P->getIncomingValueForBlock(Preheader));
    auto *Next = dyn_cast<BinaryOperator>(P->getIncomingValueForBlock(Latch));
    if (!Start || !Next || Next->getOpcode() != Instruction::Add || Next->getOperand(0) != P)
      continue;
    auto *Step = dyn_cast<ConstantInt>(Next->getOperand(1));
    if (!Step || Start->isNegative() || Step->isNegative() || Step->isZero() ||
        !Start->getValue().isIntN(15) || !Step->getValue().isIntN(15))
      continue;
    uint64_t Most = Start->getZExtValue() + Step->getZExtValue() * (Passes + 1);
    if (Most > Largest)
      continue;
    Type *I16 = Type::getInt16Ty(P->getContext());
    IRBuilder<> B(P);
    PHINode *NP = B.CreatePHI(I16, 2, P->getName() + ".z80");
    B.SetInsertPoint(Next);
    Value *NN = B.CreateAdd(NP, ConstantInt::get(I16, Step->getZExtValue()), Next->getName() + ".z80",
                            /*HasNUW=*/true, /*HasNSW=*/true);
    NP->addIncoming(ConstantInt::get(I16, Start->getZExtValue()), Preheader);
    NP->addIncoming(NN, Latch);
    rewriteUses(Next, NN, P);
    rewriteUses(P, NP, Next);
    if (Next->use_empty() || (Next->hasOneUse() && Next->user_back() == P)) {
      P->replaceAllUsesWith(PoisonValue::get(P->getType()));
      P->eraseFromParent();
      Next->eraseFromParent();
    }
    Changed = true;
  }
  return Changed;
}

bool Z80NarrowLoops::runOnFunction(Function &F) {
  LoopInfo &LI = getAnalysis<LoopInfoWrapperPass>().getLoopInfo();
  ScalarEvolution &SE = getAnalysis<ScalarEvolutionWrapperPass>().getSE();
  bool Changed = false;
  for (Loop *L : LI.getLoopsInPreorder())
    Changed |= narrowLoop(L, SE);
  return Changed;
}
