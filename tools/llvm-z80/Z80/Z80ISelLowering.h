#ifndef LLVM_LIB_TARGET_Z80_Z80ISELLOWERING_H
#define LLVM_LIB_TARGET_Z80_Z80ISELLOWERING_H

#include "llvm/CodeGen/TargetLowering.h"

namespace llvm {
class Z80Subtarget;

class Z80TargetLowering : public TargetLowering {
public:
  Z80TargetLowering(const TargetMachine &TM, const Z80Subtarget &STI);

  MVT getScalarShiftAmountTy(const DataLayout &, EVT) const override {
    return MVT::i8;
  }
  EVT getSetCCResultType(const DataLayout &, LLVMContext &, EVT VT) const override {
    return MVT::i8;
  }
  SDValue LowerOperation(SDValue Op, SelectionDAG &DAG) const override;
  bool decomposeMulByConstant(LLVMContext &Context, EVT VT,
                              SDValue C) const override;
  MachineBasicBlock *
  EmitInstrWithCustomInserter(MachineInstr &MI,
                              MachineBasicBlock *MBB) const override;

private:
  SDValue lowerShift(SDValue Op, SelectionDAG &DAG) const;
  SDValue compare(SDValue LHS, SDValue RHS, ISD::CondCode CC, const SDLoc &DL,
                  SelectionDAG &DAG, unsigned &Cond) const;

  bool CanLowerReturn(CallingConv::ID CallConv, MachineFunction &MF,
                      bool IsVarArg, const SmallVectorImpl<ISD::OutputArg> &Outs,
                      LLVMContext &Context, const Type *RetTy) const override;
  SDValue LowerReturn(SDValue Chain, CallingConv::ID CallConv, bool IsVarArg,
                      const SmallVectorImpl<ISD::OutputArg> &Outs,
                      const SmallVectorImpl<SDValue> &OutVals, const SDLoc &DL,
                      SelectionDAG &DAG) const override;
  SDValue LowerFormalArguments(SDValue Chain, CallingConv::ID CallConv,
                               bool IsVarArg,
                               const SmallVectorImpl<ISD::InputArg> &Ins,
                               const SDLoc &DL, SelectionDAG &DAG,
                               SmallVectorImpl<SDValue> &InVals) const override;
  SDValue LowerCall(CallLoweringInfo &CLI,
                    SmallVectorImpl<SDValue> &InVals) const override;
};
} // namespace llvm

#endif
