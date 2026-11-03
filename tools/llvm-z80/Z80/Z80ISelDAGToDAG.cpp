#include "Z80.h"
#include "Z80TargetMachine.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/CodeGen/SelectionDAGISel.h"

#define DEBUG_TYPE "z80-isel"
#define PASS_NAME "Z80 DAG->DAG Instruction Selection"

using namespace llvm;

namespace {
class Z80DAGToDAGISel : public SelectionDAGISel {
public:
  Z80DAGToDAGISel(Z80TargetMachine &TM, CodeGenOptLevel OptLevel)
      : SelectionDAGISel(TM, OptLevel) {}

  // A stack slot, or an outgoing argument (IX plus an offset).
  bool selectFrameAddr(SDValue N, SDValue &Base, SDValue &Disp) {
    int64_t Offset = 0;
    SDValue Root = N;
    if (N.getOpcode() == ISD::ADD) {
      auto *C = dyn_cast<ConstantSDNode>(N.getOperand(1));
      if (!C || C->getSExtValue() < -128 || C->getSExtValue() > 126)
        return false;
      Offset = C->getSExtValue();
      Root = N.getOperand(0);
    }
    if (auto *FI = dyn_cast<FrameIndexSDNode>(Root)) {
      Base = CurDAG->getTargetFrameIndex(FI->getIndex(), MVT::i16);
    } else if (auto *R = dyn_cast<RegisterSDNode>(Root)) {
      if (R->getReg() != Z80::IX)
        return false;
      Base = Root;
    } else {
      return false;
    }
    Disp = CurDAG->getTargetConstant(Offset, SDLoc(N), MVT::i8);
    return true;
  }

#include "Z80GenDAGISel.inc"

private:
  void Select(SDNode *N) override {
    if (N->isMachineOpcode()) {
      N->setNodeId(-1);
      return;
    }
    if (auto *FI = dyn_cast<FrameIndexSDNode>(N)) {
      SDValue Base = CurDAG->getTargetFrameIndex(FI->getIndex(), MVT::i16);
      SDValue Disp = CurDAG->getTargetConstant(0, SDLoc(N), MVT::i8);
      CurDAG->SelectNodeTo(N, Z80::LEA16, MVT::i16, Base, Disp);
      return;
    }
    SelectCode(N);
  }
};

class Z80DAGToDAGISelLegacy : public SelectionDAGISelLegacy {
public:
  static char ID;
  Z80DAGToDAGISelLegacy(Z80TargetMachine &TM, CodeGenOptLevel OptLevel)
      : SelectionDAGISelLegacy(
            ID, std::make_unique<Z80DAGToDAGISel>(TM, OptLevel)) {}
};
} // namespace

char Z80DAGToDAGISelLegacy::ID = 0;

INITIALIZE_PASS(Z80DAGToDAGISelLegacy, DEBUG_TYPE, PASS_NAME, false, false)

FunctionPass *llvm::createZ80ISelDag(Z80TargetMachine &TM,
                                     CodeGenOptLevel OptLevel) {
  return new Z80DAGToDAGISelLegacy(TM, OptLevel);
}
