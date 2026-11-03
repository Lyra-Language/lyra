//===-- Z80.td - Describe the Z80 target -------------------------*- tablegen -*-===//
//
// Lyra's Z80 target, for the Sega Master System and Game Gear. See ../README.md.
//
//===----------------------------------------------------------------------===//

include "llvm/Target/Target.td"

include "Z80RegisterInfo.td"
include "Z80InstrInfo.td"

defm : RemapAllTargetPseudoPointerOperands<GR16>;

def Z80InstrInfo : InstrInfo;

include "Z80CallingConv.td"

def : ProcessorModel<"z80", NoSchedModel, []>;

def Z80AsmWriter : AsmWriter {
  string AsmWriterClassName = "InstPrinter";
  bit isMCAsmWriter = 1;
}

def Z80 : Target {
  let InstructionSet = Z80InstrInfo;
  let AssemblyWriters = [Z80AsmWriter];
}
