#include "Z80MCTargetDesc.h"
#include "llvm/ADT/SmallVector.h"
#include "llvm/MC/MCCodeEmitter.h"
#include "llvm/MC/MCContext.h"
#include "llvm/MC/MCExpr.h"
#include "llvm/MC/MCFixup.h"
#include "llvm/MC/MCInst.h"
#include "llvm/MC/MCInstrInfo.h"
#include "llvm/MC/MCRegisterInfo.h"
#include "llvm/MC/MCSubtargetInfo.h"

using namespace llvm;

namespace {
class Z80MCCodeEmitter : public MCCodeEmitter {
  const MCInstrInfo &MCII;
  MCContext &Ctx;

public:
  Z80MCCodeEmitter(const MCInstrInfo &MCII, MCContext &Ctx) : MCII(MCII), Ctx(Ctx) {}

  void encodeInstruction(const MCInst &MI, SmallVectorImpl<char> &CB,
                         SmallVectorImpl<MCFixup> &Fixups,
                         const MCSubtargetInfo &STI) const override {
    unsigned Size = MCII.get(MI.getOpcode()).getSize();
    assert(Size > 0 && "a pseudo reached the code emitter");
    uint64_t Bits = getBinaryCodeForInstr(MI, Fixups, STI);
    for (int I = Size - 1; I >= 0; --I)
      CB.push_back(char((Bits >> (I * 8)) & 0xFF));
  }

  uint64_t getBinaryCodeForInstr(const MCInst &MI, SmallVectorImpl<MCFixup> &Fixups,
                                 const MCSubtargetInfo &STI) const;

  unsigned getMachineOpValue(const MCInst &MI, const MCOperand &MO,
                             SmallVectorImpl<MCFixup> &Fixups,
                             const MCSubtargetInfo &) const {
    if (MO.isReg())
      return Ctx.getRegisterInfo()->getEncodingValue(MO.getReg());
    assert(MO.isImm() && "an 8-bit operand that is not a number");
    return static_cast<unsigned>(MO.getImm());
  }

  // Always an instruction's last two bytes: low byte first.
  unsigned encodeImm16(const MCInst &MI, unsigned OpNo,
                       SmallVectorImpl<MCFixup> &Fixups,
                       const MCSubtargetInfo &) const {
    const MCOperand &MO = MI.getOperand(OpNo);
    if (MO.isExpr()) {
      unsigned Offset = MCII.get(MI.getOpcode()).getSize() - 2;
      Fixups.push_back(MCFixup::create(Offset, MO.getExpr(), FK_Data_2));
      return 0;
    }
    unsigned V = static_cast<unsigned>(MO.getImm()) & 0xFFFF;
    return ((V & 0xFF) << 8) | (V >> 8);
  }

  unsigned encodeMemIX(const MCInst &MI, unsigned OpNo, SmallVectorImpl<MCFixup> &,
                       const MCSubtargetInfo &) const {
    return static_cast<unsigned>(MI.getOperand(OpNo + 1).getImm()) & 0xFF;
  }
};
} // namespace

#include "Z80GenMCCodeEmitter.inc"

MCCodeEmitter *llvm::createZ80MCCodeEmitter(const MCInstrInfo &MCII, MCContext &Ctx) {
  return new Z80MCCodeEmitter(MCII, Ctx);
}
