#include "Z80MCTargetDesc.h"
#include "llvm/BinaryFormat/ELF.h"
#include "llvm/MC/MCAsmBackend.h"
#include "llvm/MC/MCAssembler.h"
#include "llvm/MC/MCELFObjectWriter.h"
#include "llvm/MC/MCFixup.h"
#include "llvm/MC/MCObjectWriter.h"
#include "llvm/MC/MCSubtargetInfo.h"
#include "llvm/MC/MCValue.h"
#include "llvm/Support/ErrorHandling.h"

using namespace llvm;

namespace {
// binutils' numbers for these relocations.
enum { R_Z80_8 = 1, R_Z80_16 = 4, R_Z80_32 = 6 };

class Z80ELFObjectWriter : public MCELFObjectTargetWriter {
public:
  Z80ELFObjectWriter()
      : MCELFObjectTargetWriter(false, ELF::ELFOSABI_NONE, ELF::EM_Z80, true) {}

  unsigned getRelocType(const MCFixup &Fixup, const MCValue &, bool IsPCRel) const override {
    if (IsPCRel)
      report_fatal_error("Z80: a PC-relative relocation");
    switch ((unsigned)Fixup.getKind()) {
    case FK_Data_1:
      return R_Z80_8;
    case FK_Data_2:
      return R_Z80_16;
    case FK_Data_4:
      return R_Z80_32;
    default:
      report_fatal_error("Z80: a relocation of a size it has none for");
    }
  }
};

class Z80AsmBackend : public MCAsmBackend {
public:
  Z80AsmBackend() : MCAsmBackend(llvm::endianness::little) {}

  std::unique_ptr<MCObjectTargetWriter> createObjectTargetWriter() const override {
    return std::make_unique<Z80ELFObjectWriter>();
  }

  void applyFixup(const MCFragment &F, const MCFixup &Fixup, const MCValue &Target,
                  uint8_t *Data, uint64_t Value, bool IsResolved) override {
    if (!IsResolved)
      Asm->getWriter().recordRelocation(F, Fixup, Target, Value);
    unsigned Size = getFixupKindInfo(Fixup.getKind()).TargetSize / 8;
    for (unsigned I = 0; I != Size; ++I)
      Data[I] = uint8_t(Value >> (I * 8));
  }

  bool writeNopData(raw_ostream &OS, uint64_t Count, const MCSubtargetInfo *) const override {
    OS.write_zeros(Count);
    return true;
  }
};
} // namespace

MCAsmBackend *llvm::createZ80AsmBackend(const Target &, const MCSubtargetInfo &,
                                        const MCRegisterInfo &, const MCTargetOptions &) {
  return new Z80AsmBackend();
}
