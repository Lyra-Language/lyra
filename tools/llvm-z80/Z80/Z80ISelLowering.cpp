//===-- Z80ISelLowering.cpp - lowering LLVM IR to Z80 selection DAGs -----------===//
//
// i8 and i16 are legal, in GR8 and GR16. Everything wider is LLVM's own splitting into
// i16 pieces over ADDC/ADDE. Multiplies, divides and variable shifts are calls.
//
//===----------------------------------------------------------------------===//

#include "Z80ISelLowering.h"
#include "Z80.h"
#include "Z80InstrInfo.h"
#include "Z80SelectionDAGInfo.h"
#include "Z80Subtarget.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/CodeGen/CallingConvLower.h"
#include "llvm/CodeGen/MachineFrameInfo.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/MachineRegisterInfo.h"
#include "llvm/CodeGen/SelectionDAG.h"
#include "llvm/IR/DiagnosticInfo.h"
#include "llvm/IR/Function.h"

using namespace llvm;

#define GET_CALLING_CONV_IMPL
#include "Z80GenCallingConv.inc"

Z80TargetLowering::Z80TargetLowering(const TargetMachine &TM,
                                     const Z80Subtarget &STI)
    : TargetLowering(TM, STI) {
  addRegisterClass(MVT::i8, &Z80::GR8RegClass);
  addRegisterClass(MVT::i16, &Z80::GR16RegClass);
  computeRegisterProperties(STI.getRegisterInfo());

  setBooleanContents(ZeroOrOneBooleanContent);
  setStackPointerRegisterToSaveRestore(Z80::SP);
  setSchedulingPreference(Sched::RegPressure);
  setMaxAtomicSizeInBitsSupported(0);
  setMinimumJumpTableEntries(INT_MAX);
  setMinFunctionAlignment(Align(1));
  setPrefFunctionAlignment(Align(1));

  for (MVT VT : MVT::integer_valuetypes()) {
    for (auto Ext : {ISD::EXTLOAD, ISD::ZEXTLOAD, ISD::SEXTLOAD}) {
      setLoadExtAction(Ext, VT, MVT::i1, Promote);
      setLoadExtAction(Ext, VT, MVT::i8, Expand);
    }
  }
  setTruncStoreAction(MVT::i16, MVT::i8, Expand);
  setTruncStoreAction(MVT::i16, MVT::i1, Expand);
  setTruncStoreAction(MVT::i8, MVT::i1, Expand);

  setOperationAction(ISD::GlobalAddress, MVT::i16, Custom);
  setOperationAction(ISD::ExternalSymbol, MVT::i16, Custom);
  setOperationAction(ISD::BlockAddress, MVT::i16, Custom);
  setOperationAction(ISD::BRIND, MVT::Other, Expand);
  setOperationAction(ISD::BR_JT, MVT::Other, Expand);
  setOperationAction(ISD::BRCOND, MVT::Other, Expand);
  setOperationAction(ISD::DYNAMIC_STACKALLOC, MVT::i16, Expand);
  setOperationAction(ISD::STACKSAVE, MVT::Other, Expand);
  setOperationAction(ISD::STACKRESTORE, MVT::Other, Expand);
  setOperationAction(ISD::SIGN_EXTEND_INREG, MVT::i1, Expand);
  setOperationAction(ISD::SIGN_EXTEND_INREG, MVT::i8, Expand);

  for (MVT VT : {MVT::i8, MVT::i16}) {
    setOperationAction(ISD::BR_CC, VT, Custom);
    setOperationAction(ISD::SELECT_CC, VT, Custom);
    setOperationAction(ISD::SETCC, VT, Expand);
    setOperationAction(ISD::SELECT, VT, Expand);
    setOperationAction(ISD::SHL, VT, Custom);
    setOperationAction(ISD::SRL, VT, Custom);
    setOperationAction(ISD::SRA, VT, Custom);
    setOperationAction(ISD::MUL, VT, LibCall);
    setOperationAction(ISD::SDIV, VT, LibCall);
    setOperationAction(ISD::UDIV, VT, LibCall);
    setOperationAction(ISD::SREM, VT, LibCall);
    setOperationAction(ISD::UREM, VT, LibCall);
    for (unsigned Op :
         {ISD::SDIVREM, ISD::UDIVREM, ISD::MULHS, ISD::MULHU, ISD::SMUL_LOHI,
          ISD::UMUL_LOHI, ISD::ROTL, ISD::ROTR, ISD::BSWAP, ISD::CTPOP,
          ISD::CTLZ, ISD::CTTZ, ISD::SHL_PARTS, ISD::SRL_PARTS, ISD::SRA_PARTS,
          ISD::SADDO, ISD::UADDO, ISD::SSUBO, ISD::USUBO, ISD::SMULO, ISD::UMULO})
      setOperationAction(Op, VT, Expand);
  }
  // The carry chain is for i16 alone: i32 and wider split into it.
  setOperationAction(ISD::ADDC, MVT::i16, Legal);
  setOperationAction(ISD::ADDE, MVT::i16, Legal);
  setOperationAction(ISD::SUBC, MVT::i16, Legal);
  setOperationAction(ISD::SUBE, MVT::i16, Legal);
}

// The flags for `LHS CC RHS`, and in `Cond` the condition that means it holds. The Z80 tests
// only Z and C after a compare, so a signed compare is the unsigned one with both sign bits
// flipped, and `>` and `<=` swap their operands.
SDValue Z80TargetLowering::compare(SDValue LHS, SDValue RHS, ISD::CondCode CC,
                                   const SDLoc &DL, SelectionDAG &DAG,
                                   unsigned &Cond) const {
  EVT VT = LHS.getValueType();
  if (ISD::isSignedIntSetCC(CC)) {
    SDValue Sign = DAG.getConstant(
        APInt::getSignMask(VT.getSizeInBits()), DL, VT);
    LHS = DAG.getNode(ISD::XOR, DL, VT, LHS, Sign);
    RHS = DAG.getNode(ISD::XOR, DL, VT, RHS, Sign);
    CC = ISD::getSetCCSwappedOperands(ISD::getSetCCSwappedOperands(CC));
    switch (CC) {
    case ISD::SETLT: CC = ISD::SETULT; break;
    case ISD::SETLE: CC = ISD::SETULE; break;
    case ISD::SETGT: CC = ISD::SETUGT; break;
    case ISD::SETGE: CC = ISD::SETUGE; break;
    default: llvm_unreachable("not a signed condition");
    }
  }
  switch (CC) {
  case ISD::SETEQ: Cond = Z80CC::Z; break;
  case ISD::SETNE: Cond = Z80CC::NZ; break;
  case ISD::SETULT: Cond = Z80CC::C; break;
  case ISD::SETUGE: Cond = Z80CC::NC; break;
  case ISD::SETUGT: std::swap(LHS, RHS); Cond = Z80CC::C; break;
  case ISD::SETULE: std::swap(LHS, RHS); Cond = Z80CC::NC; break;
  default: llvm_unreachable("a condition an integer compare does not have");
  }
  return DAG.getNode(Z80ISD::CMP, DL, MVT::Glue, LHS, RHS);
}

// A constant amount is that many shifts by one, in nodes the combiner will not fold back
// into the shift they came from. A variable one is a call; for i8, on the value widened.
SDValue Z80TargetLowering::lowerShift(SDValue Op, SelectionDAG &DAG) const {
  SDLoc DL(Op);
  EVT VT = Op.getValueType();
  SDValue Value = Op.getOperand(0);
  auto *Amount = dyn_cast<ConstantSDNode>(Op.getOperand(1));
  if (!Amount) {
    if (VT != MVT::i8) {
      RTLIB::Libcall Call = Op.getOpcode() == ISD::SHL   ? RTLIB::SHL_I16
                            : Op.getOpcode() == ISD::SRL ? RTLIB::SRL_I16
                                                         : RTLIB::SRA_I16;
      SDValue Ops[] = {Value, Op.getOperand(1)};
      return makeLibCall(DAG, Call, VT, Ops, MakeLibCallOptions(), DL).first;
    }
    unsigned Ext = Op.getOpcode() == ISD::SRA ? ISD::SIGN_EXTEND : ISD::ZERO_EXTEND;
    SDValue Wide = DAG.getNode(Ext, DL, MVT::i16, Value);
    SDValue Shifted = DAG.getNode(Op.getOpcode(), DL, MVT::i16, Wide, Op.getOperand(1));
    return DAG.getNode(ISD::TRUNCATE, DL, MVT::i8, Shifted);
  }
  unsigned Node = Op.getOpcode() == ISD::SHL   ? Z80ISD::LSL
                  : Op.getOpcode() == ISD::SRL ? Z80ISD::LSR
                                               : Z80ISD::ASR;
  uint64_t N = Amount->getZExtValue() & (VT.getSizeInBits() - 1);
  for (uint64_t I = 0; I < N; ++I)
    Value = DAG.getNode(Node, DL, VT, Value);
  return Value;
}

SDValue Z80TargetLowering::LowerOperation(SDValue Op, SelectionDAG &DAG) const {
  SDLoc DL(Op);
  switch (Op.getOpcode()) {
  default:
    llvm_unreachable("an operation Z80 lowering was not told about");
  case ISD::GlobalAddress: {
    auto *G = cast<GlobalAddressSDNode>(Op);
    SDValue T = DAG.getTargetGlobalAddress(G->getGlobal(), DL, MVT::i16, G->getOffset());
    return DAG.getNode(Z80ISD::WRAPPER, DL, MVT::i16, T);
  }
  case ISD::ExternalSymbol: {
    auto *S = cast<ExternalSymbolSDNode>(Op);
    SDValue T = DAG.getTargetExternalSymbol(S->getSymbol(), MVT::i16);
    return DAG.getNode(Z80ISD::WRAPPER, DL, MVT::i16, T);
  }
  case ISD::BlockAddress: {
    auto *B = cast<BlockAddressSDNode>(Op);
    SDValue T = DAG.getTargetBlockAddress(B->getBlockAddress(), MVT::i16, B->getOffset());
    return DAG.getNode(Z80ISD::WRAPPER, DL, MVT::i16, T);
  }
  case ISD::BR_CC: {
    unsigned Cond;
    SDValue Flags = compare(Op.getOperand(2), Op.getOperand(3),
                            cast<CondCodeSDNode>(Op.getOperand(1))->get(), DL, DAG, Cond);
    return DAG.getNode(Z80ISD::BRCOND, DL, MVT::Other, Op.getOperand(0), Op.getOperand(4),
                       DAG.getTargetConstant(Cond, DL, MVT::i8), Flags);
  }
  case ISD::SELECT_CC: {
    unsigned Cond;
    SDValue Flags = compare(Op.getOperand(0), Op.getOperand(1),
                            cast<CondCodeSDNode>(Op.getOperand(4))->get(), DL, DAG, Cond);
    return DAG.getNode(Z80ISD::SELECT, DL, Op.getValueType(), Op.getOperand(2),
                       Op.getOperand(3), DAG.getTargetConstant(Cond, DL, MVT::i8), Flags);
  }
  case ISD::SHL:
  case ISD::SRL:
  case ISD::SRA:
    return lowerShift(Op, DAG);
  }
}

MachineBasicBlock *
Z80TargetLowering::EmitInstrWithCustomInserter(MachineInstr &MI,
                                               MachineBasicBlock *BB) const {
  assert((MI.getOpcode() == Z80::SELECT8 || MI.getOpcode() == Z80::SELECT16) &&
         "only selects have a custom inserter");
  const TargetInstrInfo &TII = *BB->getParent()->getSubtarget().getInstrInfo();
  DebugLoc DL = MI.getDebugLoc();
  MachineFunction *MF = BB->getParent();
  const BasicBlock *LLVMBB = BB->getBasicBlock();
  MachineFunction::iterator Next = ++BB->getIterator();

  MachineBasicBlock *False = MF->CreateMachineBasicBlock(LLVMBB);
  MachineBasicBlock *Sink = MF->CreateMachineBasicBlock(LLVMBB);
  MF->insert(Next, False);
  MF->insert(Next, Sink);
  Sink->splice(Sink->begin(), BB, std::next(MI.getIterator()), BB->end());
  Sink->transferSuccessorsAndUpdatePHIs(BB);
  BB->addSuccessor(False);
  BB->addSuccessor(Sink);
  False->addSuccessor(Sink);

  BuildMI(BB, DL, TII.get(Z80::JPcc)).addMBB(Sink).addImm(MI.getOperand(3).getImm());
  BuildMI(*Sink, Sink->begin(), DL, TII.get(TargetOpcode::PHI), MI.getOperand(0).getReg())
      .addReg(MI.getOperand(1).getReg())
      .addMBB(BB)
      .addReg(MI.getOperand(2).getReg())
      .addMBB(False);
  MI.eraseFromParent();
  return Sink;
}

static void refuseVarArg(bool IsVarArg, SelectionDAG &DAG, const SDLoc &DL) {
  if (IsVarArg)
    DAG.getContext()->diagnose(DiagnosticInfoUnsupported(
        DAG.getMachineFunction().getFunction(),
        "the Z80 target has no variadic functions", DL.getDebugLoc()));
}

bool Z80TargetLowering::CanLowerReturn(
    CallingConv::ID, MachineFunction &MF, bool IsVarArg,
    const SmallVectorImpl<ISD::OutputArg> &Outs, LLVMContext &Context,
    const Type *) const {
  SmallVector<CCValAssign, 8> RVLocs;
  CCState CCInfo(CallingConv::C, IsVarArg, MF, RVLocs, Context);
  return CCInfo.CheckReturn(Outs, RetCC_Z80);
}

SDValue Z80TargetLowering::LowerReturn(
    SDValue Chain, CallingConv::ID, bool IsVarArg,
    const SmallVectorImpl<ISD::OutputArg> &Outs,
    const SmallVectorImpl<SDValue> &OutVals, const SDLoc &DL,
    SelectionDAG &DAG) const {
  MachineFunction &MF = DAG.getMachineFunction();
  SmallVector<CCValAssign, 8> RVLocs;
  CCState CCInfo(CallingConv::C, IsVarArg, MF, RVLocs, *DAG.getContext());
  CCInfo.AnalyzeReturn(Outs, RetCC_Z80);

  SDValue Glue;
  SmallVector<SDValue, 8> Ops(1, Chain);
  for (unsigned I = 0, E = RVLocs.size(); I != E; ++I) {
    CCValAssign &VA = RVLocs[I];
    SDValue Value = OutVals[I];
    if (VA.getLocInfo() == CCValAssign::SExt)
      Value = DAG.getNode(ISD::SIGN_EXTEND, DL, VA.getLocVT(), Value);
    else if (VA.getLocInfo() == CCValAssign::ZExt)
      Value = DAG.getNode(ISD::ZERO_EXTEND, DL, VA.getLocVT(), Value);
    else if (VA.getLocInfo() == CCValAssign::AExt)
      Value = DAG.getNode(ISD::ANY_EXTEND, DL, VA.getLocVT(), Value);
    Chain = DAG.getCopyToReg(Chain, DL, VA.getLocReg(), Value, Glue);
    Glue = Chain.getValue(1);
    Ops.push_back(DAG.getRegister(VA.getLocReg(), VA.getLocVT()));
  }
  Ops[0] = Chain;
  if (Glue.getNode())
    Ops.push_back(Glue);
  return DAG.getNode(Z80ISD::RET, DL, MVT::Other, Ops);
}

// A value as its argument location holds it, back to the type it was.
static SDValue fromLocation(SDValue Value, const CCValAssign &VA,
                            SelectionDAG &DAG, const SDLoc &DL) {
  switch (VA.getLocInfo()) {
  case CCValAssign::SExt:
    Value = DAG.getNode(ISD::AssertSext, DL, VA.getLocVT(), Value,
                        DAG.getValueType(VA.getValVT()));
    break;
  case CCValAssign::ZExt:
    Value = DAG.getNode(ISD::AssertZext, DL, VA.getLocVT(), Value,
                        DAG.getValueType(VA.getValVT()));
    break;
  default:
    break;
  }
  if (VA.getLocInfo() != CCValAssign::Full)
    Value = DAG.getNode(ISD::TRUNCATE, DL, VA.getValVT(), Value);
  return Value;
}

SDValue Z80TargetLowering::LowerFormalArguments(
    SDValue Chain, CallingConv::ID, bool IsVarArg,
    const SmallVectorImpl<ISD::InputArg> &Ins, const SDLoc &DL,
    SelectionDAG &DAG, SmallVectorImpl<SDValue> &InVals) const {
  refuseVarArg(IsVarArg, DAG, DL);
  MachineFunction &MF = DAG.getMachineFunction();
  MachineFrameInfo &MFI = MF.getFrameInfo();
  SmallVector<CCValAssign, 8> ArgLocs;
  CCState CCInfo(CallingConv::C, IsVarArg, MF, ArgLocs, *DAG.getContext());
  CCInfo.AnalyzeFormalArguments(Ins, CC_Z80);

  for (const CCValAssign &VA : ArgLocs) {
    SDValue Value;
    if (VA.isRegLoc()) {
      const TargetRegisterClass *RC =
          VA.getLocVT() == MVT::i8 ? &Z80::GR8RegClass : &Z80::GR16RegClass;
      Register V = MF.addLiveIn(VA.getLocReg(), RC);
      Value = DAG.getCopyFromReg(Chain, DL, V, VA.getLocVT());
    } else {
      // Above the return address.
      int FI = MFI.CreateFixedObject(VA.getLocVT().getStoreSize(),
                                     VA.getLocMemOffset() + 2, true);
      Value = DAG.getLoad(VA.getLocVT(), DL, Chain,
                          DAG.getFrameIndex(FI, MVT::i16),
                          MachinePointerInfo::getFixedStack(MF, FI));
    }
    InVals.push_back(fromLocation(Value, VA, DAG, DL));
  }
  return Chain;
}

SDValue Z80TargetLowering::LowerCall(CallLoweringInfo &CLI,
                                     SmallVectorImpl<SDValue> &InVals) const {
  SelectionDAG &DAG = CLI.DAG;
  SDLoc &DL = CLI.DL;
  SDValue Chain = CLI.Chain;
  SDValue Callee = CLI.Callee;
  MachineFunction &MF = DAG.getMachineFunction();
  CLI.IsTailCall = false;
  refuseVarArg(CLI.IsVarArg, DAG, DL);

  SmallVector<CCValAssign, 8> ArgLocs;
  CCState CCInfo(CallingConv::C, CLI.IsVarArg, MF, ArgLocs, *DAG.getContext());
  CCInfo.AnalyzeCallOperands(CLI.Outs, CC_Z80);
  unsigned StackBytes = CCInfo.getStackSize();

  Chain = DAG.getCALLSEQ_START(Chain, StackBytes, 0, DL);

  // The stack arguments go in the frame's reserved bottom, which is (ix+0) on.
  SmallVector<std::pair<Register, SDValue>, 4> RegArgs;
  SmallVector<SDValue, 8> Stores;
  SDValue IX = DAG.getRegister(Z80::IX, MVT::i16);
  for (unsigned I = 0, E = ArgLocs.size(); I != E; ++I) {
    CCValAssign &VA = ArgLocs[I];
    SDValue Value = CLI.OutVals[I];
    if (VA.getLocInfo() == CCValAssign::SExt)
      Value = DAG.getNode(ISD::SIGN_EXTEND, DL, VA.getLocVT(), Value);
    else if (VA.getLocInfo() == CCValAssign::ZExt)
      Value = DAG.getNode(ISD::ZERO_EXTEND, DL, VA.getLocVT(), Value);
    else if (VA.getLocInfo() == CCValAssign::AExt)
      Value = DAG.getNode(ISD::ANY_EXTEND, DL, VA.getLocVT(), Value);
    if (VA.isRegLoc()) {
      RegArgs.push_back({VA.getLocReg(), Value});
    } else {
      SDValue Address = DAG.getNode(ISD::ADD, DL, MVT::i16, IX,
                                    DAG.getConstant(VA.getLocMemOffset(), DL, MVT::i16));
      Stores.push_back(DAG.getStore(Chain, DL, Value, Address, MachinePointerInfo()));
    }
  }
  if (!Stores.empty())
    Chain = DAG.getNode(ISD::TokenFactor, DL, MVT::Other, Stores);

  // The Z80 has no `call (hl)`: an indirect call goes through `__z80_callbc`, which is
  // `push bc; ret`. BC carries no argument.
  bool Indirect = false;
  if (auto *G = dyn_cast<GlobalAddressSDNode>(Callee)) {
    Callee = DAG.getTargetGlobalAddress(G->getGlobal(), DL, MVT::i16, G->getOffset());
  } else if (auto *S = dyn_cast<ExternalSymbolSDNode>(Callee)) {
    Callee = DAG.getTargetExternalSymbol(S->getSymbol(), MVT::i16);
  } else {
    RegArgs.push_back({Z80::BC, Callee});
    Callee = DAG.getTargetExternalSymbol("__z80_callbc", MVT::i16);
    Indirect = true;
  }
  (void)Indirect;

  SDValue Glue;
  for (auto &[Reg, Value] : RegArgs) {
    Chain = DAG.getCopyToReg(Chain, DL, Reg, Value, Glue);
    Glue = Chain.getValue(1);
  }

  SmallVector<SDValue, 8> Ops = {Chain, Callee};
  for (auto &[Reg, Value] : RegArgs)
    Ops.push_back(DAG.getRegister(Reg, Value.getValueType()));
  const uint32_t *Mask =
      MF.getSubtarget().getRegisterInfo()->getCallPreservedMask(MF, CLI.CallConv);
  Ops.push_back(DAG.getRegisterMask(Mask));
  if (Glue.getNode())
    Ops.push_back(Glue);
  Chain = DAG.getNode(Z80ISD::CALL, DL, DAG.getVTList(MVT::Other, MVT::Glue), Ops);
  Glue = Chain.getValue(1);

  Chain = DAG.getCALLSEQ_END(Chain, StackBytes, 0, Glue, DL);
  Glue = Chain.getValue(1);

  SmallVector<CCValAssign, 8> RVLocs;
  CCState RetInfo(CallingConv::C, CLI.IsVarArg, MF, RVLocs, *DAG.getContext());
  RetInfo.AnalyzeCallResult(CLI.Ins, RetCC_Z80);
  for (const CCValAssign &VA : RVLocs) {
    SDValue Value = DAG.getCopyFromReg(Chain, DL, VA.getLocReg(), VA.getLocVT(), Glue);
    Chain = Value.getValue(1);
    Glue = Value.getValue(2);
    InVals.push_back(fromLocation(Value, VA, DAG, DL));
  }
  return Chain;
}
