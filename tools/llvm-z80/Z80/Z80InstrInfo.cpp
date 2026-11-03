#include "Z80InstrInfo.h"
#include "Z80.h"
#include "Z80Subtarget.h"
#include "MCTargetDesc/Z80MCTargetDesc.h"
#include "llvm/CodeGen/MachineFrameInfo.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/MachineMemOperand.h"
#include "llvm/Support/ErrorHandling.h"

#define GET_INSTRINFO_CTOR_DTOR
#include "Z80GenInstrInfo.inc"

using namespace llvm;

Z80InstrInfo::Z80InstrInfo(const Z80Subtarget &STI)
    : Z80GenInstrInfo(STI, RI, Z80::ADJCALLSTACKDOWN, Z80::ADJCALLSTACKUP),
      RI() {}

void Z80InstrInfo::copyPhysReg(MachineBasicBlock &MBB,
                               MachineBasicBlock::iterator MI,
                               const DebugLoc &DL, Register DestReg,
                               Register SrcReg, bool KillSrc, bool, bool) const {
  if (Z80::GR8RegClass.contains(DestReg, SrcReg)) {
    BuildMI(MBB, MI, DL, get(Z80::LD8rr), DestReg)
        .addReg(SrcReg, getKillRegState(KillSrc));
    return;
  }
  if (Z80::GR16RegClass.contains(DestReg, SrcReg)) {
    BuildMI(MBB, MI, DL, get(Z80::LD8rr), RI.getSubReg(DestReg, Z80::sub_lo))
        .addReg(RI.getSubReg(SrcReg, Z80::sub_lo), getKillRegState(KillSrc));
    BuildMI(MBB, MI, DL, get(Z80::LD8rr), RI.getSubReg(DestReg, Z80::sub_hi))
        .addReg(RI.getSubReg(SrcReg, Z80::sub_hi), getKillRegState(KillSrc));
    return;
  }
  report_fatal_error("Z80: a copy between registers it cannot copy");
}

static MachineMemOperand *slotMemOperand(MachineFunction &MF, int FI,
                                         MachineMemOperand::Flags Flags) {
  const MachineFrameInfo &MFI = MF.getFrameInfo();
  return MF.getMachineMemOperand(MachinePointerInfo::getFixedStack(MF, FI),
                                 Flags, MFI.getObjectSize(FI),
                                 MFI.getObjectAlign(FI));
}

void Z80InstrInfo::storeRegToStackSlot(
    MachineBasicBlock &MBB, MachineBasicBlock::iterator MI, Register SrcReg,
    bool isKill, int FrameIndex, const TargetRegisterClass *RC, Register,
    MachineInstr::MIFlag Flags) const {
  MachineFunction &MF = *MBB.getParent();
  unsigned Opc;
  if (Z80::GR8RegClass.hasSubClassEq(RC))
    Opc = Z80::ST8fr;
  else if (Z80::GR16RegClass.hasSubClassEq(RC))
    Opc = Z80::ST16fr;
  else
    report_fatal_error("Z80: a register it cannot spill");
  BuildMI(MBB, MI, MBB.findDebugLoc(MI), get(Opc))
      .addFrameIndex(FrameIndex)
      .addImm(0)
      .addReg(SrcReg, getKillRegState(isKill))
      .addMemOperand(slotMemOperand(MF, FrameIndex, MachineMemOperand::MOStore))
      .setMIFlag(Flags);
}

void Z80InstrInfo::loadRegFromStackSlot(
    MachineBasicBlock &MBB, MachineBasicBlock::iterator MI, Register DestReg,
    int FrameIndex, const TargetRegisterClass *RC, Register, unsigned,
    MachineInstr::MIFlag Flags) const {
  MachineFunction &MF = *MBB.getParent();
  unsigned Opc;
  if (Z80::GR8RegClass.hasSubClassEq(RC))
    Opc = Z80::LD8rf;
  else if (Z80::GR16RegClass.hasSubClassEq(RC))
    Opc = Z80::LD16rf;
  else
    report_fatal_error("Z80: a register it cannot reload");
  BuildMI(MBB, MI, MBB.findDebugLoc(MI), get(Opc), DestReg)
      .addFrameIndex(FrameIndex)
      .addImm(0)
      .addMemOperand(slotMemOperand(MF, FrameIndex, MachineMemOperand::MOLoad))
      .setMIFlag(Flags);
}

bool Z80InstrInfo::analyzeBranch(MachineBasicBlock &MBB,
                                 MachineBasicBlock *&TBB,
                                 MachineBasicBlock *&FBB,
                                 SmallVectorImpl<MachineOperand> &Cond,
                                 bool) const {
  MachineBasicBlock::iterator I = MBB.getLastNonDebugInstr();
  if (I == MBB.end() || !isUnpredicatedTerminator(*I))
    return false;
  // A return, or anything else this does not know, is not analyzed.
  if (I->getOpcode() == Z80::JP) {
    MachineBasicBlock *Last = I->getOperand(0).getMBB();
    if (I == MBB.begin() || !isUnpredicatedTerminator(*std::prev(I))) {
      TBB = Last;
      return false;
    }
    MachineBasicBlock::iterator P = std::prev(I);
    if (P->getOpcode() != Z80::JPcc ||
        (P != MBB.begin() && isUnpredicatedTerminator(*std::prev(P))))
      return true;
    TBB = P->getOperand(0).getMBB();
    Cond.push_back(P->getOperand(1));
    FBB = Last;
    return false;
  }
  if (I->getOpcode() == Z80::JPcc) {
    if (I != MBB.begin() && isUnpredicatedTerminator(*std::prev(I)))
      return true;
    TBB = I->getOperand(0).getMBB();
    Cond.push_back(I->getOperand(1));
    return false;
  }
  return true;
}

unsigned Z80InstrInfo::insertBranch(MachineBasicBlock &MBB,
                                    MachineBasicBlock *TBB,
                                    MachineBasicBlock *FBB,
                                    ArrayRef<MachineOperand> Cond,
                                    const DebugLoc &DL, int *BytesAdded) const {
  unsigned Count = 0;
  if (Cond.empty()) {
    BuildMI(&MBB, DL, get(Z80::JP)).addMBB(TBB);
    Count = 1;
  } else {
    BuildMI(&MBB, DL, get(Z80::JPcc)).addMBB(TBB).addImm(Cond[0].getImm());
    Count = 1;
    if (FBB) {
      BuildMI(&MBB, DL, get(Z80::JP)).addMBB(FBB);
      Count = 2;
    }
  }
  if (BytesAdded)
    *BytesAdded = Count * 3;
  return Count;
}

unsigned Z80InstrInfo::removeBranch(MachineBasicBlock &MBB,
                                    int *BytesRemoved) const {
  unsigned Count = 0;
  MachineBasicBlock::iterator I = MBB.end();
  while (I != MBB.begin()) {
    --I;
    if (I->isDebugInstr())
      continue;
    if (I->getOpcode() != Z80::JP && I->getOpcode() != Z80::JPcc)
      break;
    I->eraseFromParent();
    I = MBB.end();
    ++Count;
  }
  if (BytesRemoved)
    *BytesRemoved = Count * 3;
  return Count;
}

bool Z80InstrInfo::reverseBranchCondition(
    SmallVectorImpl<MachineOperand> &Cond) const {
  Cond[0].setImm(Cond[0].getImm() ^ 1);
  return false;
}

bool Z80InstrInfo::expandPostRAPseudo(MachineInstr &MI) const {
  MachineBasicBlock &MBB = *MI.getParent();
  DebugLoc DL = MI.getDebugLoc();
  auto Build = [&](unsigned Opc) { return BuildMI(MBB, MI, DL, get(Opc)); };
  auto BuildTo = [&](unsigned Opc, Register D) {
    return BuildMI(MBB, MI, DL, get(Opc), D);
  };
  auto Lo = [&](Register R) -> Register { return RI.getSubReg(R, Z80::sub_lo); };
  auto Hi = [&](Register R) -> Register { return RI.getSubReg(R, Z80::sub_hi); };
  auto Move = [&](Register D, Register S) {
    if (D != S)
      BuildTo(Z80::LD8rr, D).addReg(S);
  };
  // `op` of a pair's byte, the result staying in that byte: read through A.
  auto ByteOp = [&](unsigned Opc, Register D, Register S) {
    Move(Z80::A, D);
    BuildTo(Opc, Z80::A).addReg(Z80::A).addReg(S);
    Move(D, Z80::A);
  };

  switch (MI.getOpcode()) {
  default:
    return false;
  case Z80::LD16rp: {
    Register D = MI.getOperand(0).getReg();
    if (D == Z80::HL) {
      BuildTo(Z80::LD8rp, Z80::A).addReg(Z80::HL);
      BuildTo(Z80::INC16, Z80::HL).addReg(Z80::HL);
      BuildTo(Z80::LD8rp, Z80::H).addReg(Z80::HL);
      Move(Z80::L, Z80::A);
    } else {
      BuildTo(Z80::LD8rp, Lo(D)).addReg(Z80::HL);
      BuildTo(Z80::INC16, Z80::HL).addReg(Z80::HL);
      BuildTo(Z80::LD8rp, Hi(D)).addReg(Z80::HL);
      BuildTo(Z80::DEC16, Z80::HL).addReg(Z80::HL);
    }
    break;
  }
  case Z80::ST16pr: {
    Register S = MI.getOperand(1).getReg();
    Build(Z80::ST8pr).addReg(Z80::HL).addReg(Lo(S));
    BuildTo(Z80::INC16, Z80::HL).addReg(Z80::HL);
    Build(Z80::ST8pr).addReg(Z80::HL).addReg(Hi(S));
    BuildTo(Z80::DEC16, Z80::HL).addReg(Z80::HL);
    break;
  }
  case Z80::LD16rf: {
    Register D = MI.getOperand(0).getReg();
    int64_t Disp = MI.getOperand(2).getImm();
    BuildTo(Z80::LD8rf, Lo(D)).addReg(Z80::IX).addImm(Disp);
    BuildTo(Z80::LD8rf, Hi(D)).addReg(Z80::IX).addImm(Disp + 1);
    break;
  }
  case Z80::ST16fr: {
    int64_t Disp = MI.getOperand(1).getImm();
    Register S = MI.getOperand(2).getReg();
    Build(Z80::ST8fr).addReg(Z80::IX).addImm(Disp).addReg(Lo(S));
    Build(Z80::ST8fr).addReg(Z80::IX).addImm(Disp + 1).addReg(Hi(S));
    break;
  }
  case Z80::LEA16:
    // SP and IX are the same through the body, and SP takes no 8-bit limit on the offset.
    BuildTo(Z80::LD16ri, Z80::HL).addImm(MI.getOperand(2).getImm() & 0xFFFF);
    BuildTo(Z80::ADDHLSP, Z80::HL).addReg(Z80::HL);
    break;
  case Z80::CP16: {
    Register B = MI.getOperand(1).getReg();
    Build(Z80::ORAA);
    BuildTo(Z80::SBC16, Z80::HL).addReg(Z80::HL).addReg(B);
    BuildTo(Z80::ADD16, Z80::HL).addReg(Z80::HL).addReg(B);
    break;
  }
  case Z80::SUB16: {
    Register R = MI.getOperand(2).getReg();
    Build(Z80::ORAA);
    BuildTo(Z80::SBC16, Z80::HL).addReg(Z80::HL).addReg(R);
    break;
  }
  case Z80::AND16:
  case Z80::OR16:
  case Z80::XOR16: {
    unsigned Opc = MI.getOpcode() == Z80::AND16  ? Z80::AND8r
                   : MI.getOpcode() == Z80::OR16 ? Z80::OR8r
                                                 : Z80::XOR8r;
    Register D = MI.getOperand(0).getReg();
    Register B = MI.getOperand(2).getReg();
    ByteOp(Opc, Lo(D), Lo(B));
    ByteOp(Opc, Hi(D), Hi(B));
    break;
  }
  case Z80::LSL16: {
    Register D = MI.getOperand(0).getReg();
    BuildTo(Z80::SLA8, Lo(D)).addReg(Lo(D));
    BuildTo(Z80::RL8, Hi(D)).addReg(Hi(D));
    break;
  }
  case Z80::LSR16:
  case Z80::ASR16: {
    Register D = MI.getOperand(0).getReg();
    unsigned Opc = MI.getOpcode() == Z80::LSR16 ? Z80::SRL8 : Z80::SRA8;
    BuildTo(Opc, Hi(D)).addReg(Hi(D));
    BuildTo(Z80::RR8, Lo(D)).addReg(Lo(D));
    break;
  }
  case Z80::ZEXT16: {
    Register D = MI.getOperand(0).getReg();
    Move(Lo(D), MI.getOperand(1).getReg());
    BuildTo(Z80::LD8ri, Hi(D)).addImm(0);
    break;
  }
  case Z80::SEXT16: {
    Register D = MI.getOperand(0).getReg();
    Move(Lo(D), MI.getOperand(1).getReg());
    Move(Z80::A, Lo(D));
    Build(Z80::RLCA);
    Build(Z80::SBCAA);
    Move(Hi(D), Z80::A);
    break;
  }
  }
  MI.eraseFromParent();
  return true;
}
