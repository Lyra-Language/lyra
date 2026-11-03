//===-- Z80InstrInfo.td - Z80 instructions -----------------------*- tablegen -*-===//
//
// An instruction's bytes are `Inst`, its first byte highest; a 16-bit operand is written
// little-endian by its encoder. Pseudos are what the Z80 has no one instruction for, expanded
// by Z80InstrInfo::expandPostRAPseudo.
//
//===----------------------------------------------------------------------===//

//===----------------------------------------------------------------------===//
// Nodes
//===----------------------------------------------------------------------===//

def Z80wrapper : SDNode<"Z80ISD::WRAPPER",
                        SDTypeProfile<1, 1, [SDTCisSameAs<0, 1>, SDTCisPtrTy<0>]>>;
def Z80cmp : SDNode<"Z80ISD::CMP", SDTypeProfile<0, 2, [SDTCisSameAs<0, 1>]>,
                    [SDNPOutGlue]>;
def Z80brcond : SDNode<"Z80ISD::BRCOND",
                       SDTypeProfile<0, 2, [SDTCisVT<0, OtherVT>, SDTCisVT<1, i8>]>,
                       [SDNPHasChain, SDNPInGlue]>;
def Z80select : SDNode<"Z80ISD::SELECT",
                       SDTypeProfile<1, 3, [SDTCisSameAs<0, 1>, SDTCisSameAs<1, 2>,
                                            SDTCisVT<3, i8>]>,
                       [SDNPInGlue]>;
def Z80call : SDNode<"Z80ISD::CALL", SDTypeProfile<0, -1, [SDTCisPtrTy<0>]>,
                     [SDNPHasChain, SDNPOptInGlue, SDNPOutGlue, SDNPVariadic]>;
def Z80ret : SDNode<"Z80ISD::RET", SDTNone,
                    [SDNPHasChain, SDNPOptInGlue, SDNPVariadic]>;
// A checked add or subtract: the value, and whether it overflowed (Z80ConstrainedCopies makes
// the jump on that flag directly, and the flag materialized is the rare case).
def SDTZ80Overflow : SDTypeProfile<2, 2, [SDTCisSameAs<0, 2>, SDTCisSameAs<0, 3>, SDTCisVT<1, i8>]>;
def Z80saddo : SDNode<"Z80ISD::SADDO", SDTZ80Overflow>;
def Z80ssubo : SDNode<"Z80ISD::SSUBO", SDTZ80Overflow>;
def Z80uaddo : SDNode<"Z80ISD::UADDO", SDTZ80Overflow>;
def Z80usubo : SDNode<"Z80ISD::USUBO", SDTZ80Overflow>;
// Whether a value is zero, and its sign, without a second operand.
def Z80testz : SDNode<"Z80ISD::TESTZ", SDTypeProfile<0, 1, []>, [SDNPOutGlue]>;
def Z80tests : SDNode<"Z80ISD::TESTS", SDTypeProfile<0, 1, []>, [SDNPOutGlue]>;
def Z80hibyte : SDNode<"Z80ISD::HIBYTE", SDTypeProfile<1, 1, [SDTCisVT<0, i8>, SDTCisVT<1, i16>]>>;
def Z80byteup : SDNode<"Z80ISD::BYTEUP", SDTIntUnaryOp>;
def Z80lsl : SDNode<"Z80ISD::LSL", SDTIntUnaryOp>;
def Z80lsr : SDNode<"Z80ISD::LSR", SDTIntUnaryOp>;
def Z80asr : SDNode<"Z80ISD::ASR", SDTIntUnaryOp>;

def callseq_start : SDNode<"ISD::CALLSEQ_START",
                           SDCallSeqStart<[SDTCisVT<0, i16>, SDTCisVT<1, i16>]>,
                           [SDNPHasChain, SDNPOutGlue]>;
def callseq_end : SDNode<"ISD::CALLSEQ_END",
                         SDCallSeqEnd<[SDTCisVT<0, i16>, SDTCisVT<1, i16>]>,
                         [SDNPHasChain, SDNPOptInGlue, SDNPOutGlue]>;

//===----------------------------------------------------------------------===//
// Operands
//===----------------------------------------------------------------------===//

def imm16op : Operand<i16> {
  let EncoderMethod = "encodeImm16";
}
def brtarget : Operand<OtherVT> {
  let EncoderMethod = "encodeImm16";
}
def ccop : Operand<i8> {
  let PrintMethod = "printCC";
}
// A stack slot: IX and a displacement, a frame index until frame lowering.
def memix : Operand<i16> {
  let MIOperandInfo = (ops IX16:$base, i8imm:$disp);
  let PrintMethod = "printMemIX";
  let EncoderMethod = "encodeMemIX";
}
def addr_ix : ComplexPattern<i16, 2, "selectFrameAddr", [frameindex, add]>;

//===----------------------------------------------------------------------===//
// Formats
//===----------------------------------------------------------------------===//

class Z80Inst<int size, dag outs, dag ins, string asm, list<dag> pattern = []>
    : Instruction {
  let Namespace = "Z80";
  field bits<32> Inst;
  field bits<32> SoftFail = 0;
  let Size = size;
  let OutOperandList = outs;
  let InOperandList = ins;
  let AsmString = asm;
  let Pattern = pattern;
  let hasSideEffects = 0;
}

class Z80Pseudo<dag outs, dag ins, list<dag> pattern = []>
    : Z80Inst<0, outs, ins, "", pattern> {
  let isPseudo = 1;
  let isCodeGenOnly = 1;
}

//===----------------------------------------------------------------------===//
// Moves
//===----------------------------------------------------------------------===//

let isMoveReg = 1 in
def LD8rr : Z80Inst<1, (outs GR8:$d), (ins GR8:$s), "ld\t$d, $s"> {
  bits<3> d;
  bits<3> s;
  let Inst{7-6} = 0b01;
  let Inst{5-3} = d;
  let Inst{2-0} = s;
}

let isReMaterializable = 1, isAsCheapAsAMove = 1 in {
  def LD8ri : Z80Inst<2, (outs GR8:$d), (ins i8imm:$n), "ld\t$d, $n",
                      [(set GR8:$d, imm:$n)]> {
    bits<3> d;
    bits<8> n;
    let Inst{15-14} = 0b00;
    let Inst{13-11} = d;
    let Inst{10-8} = 0b110;
    let Inst{7-0} = n;
  }
  def LD16ri : Z80Inst<3, (outs GR16:$d), (ins imm16op:$n), "ld\t$d, $n",
                       [(set GR16:$d, imm:$n)]> {
    bits<2> d;
    bits<16> n;
    let Inst{23-22} = 0b00;
    let Inst{21-20} = d;
    let Inst{19-16} = 0b0001;
    let Inst{15-0} = n;
  }
}
def : Pat<(Z80wrapper tglobaladdr:$a), (LD16ri tglobaladdr:$a)>;
def : Pat<(Z80wrapper texternalsym:$a), (LD16ri texternalsym:$a)>;
def : Pat<(Z80wrapper tblockaddress:$a), (LD16ri tblockaddress:$a)>;

//===----------------------------------------------------------------------===//
// Memory
//===----------------------------------------------------------------------===//

let mayLoad = 1 in {
  def LD8rp : Z80Inst<1, (outs GR8:$d), (ins HL16:$p), "ld\t$d, (hl)",
                      [(set GR8:$d, (load HL16:$p))]> {
    bits<3> d;
    let Inst{7-6} = 0b01;
    let Inst{5-3} = d;
    let Inst{2-0} = 0b110;
  }
  def LD8rf : Z80Inst<3, (outs GR8:$d), (ins memix:$m), "ld\t$d, $m",
                      [(set GR8:$d, (load addr_ix:$m))]> {
    bits<3> d;
    bits<8> m;
    let Inst{23-16} = 0xDD;
    let Inst{15-14} = 0b01;
    let Inst{13-11} = d;
    let Inst{10-8} = 0b110;
    let Inst{7-0} = m;
  }
  def LD8ra : Z80Inst<3, (outs ACC8:$d), (ins imm16op:$a), "ld\ta, ($a)"> {
    bits<16> a;
    let Inst{23-16} = 0x3A;
    let Inst{15-0} = a;
  }
  def LD16ra : Z80Inst<3, (outs HL16:$d), (ins imm16op:$a), "ld\thl, ($a)"> {
    bits<16> a;
    let Inst{23-16} = 0x2A;
    let Inst{15-0} = a;
  }
  // Through HL, left as it was unless it is the destination. Clobbers A for that case.
  let Defs = [A] in
  def LD16rp : Z80Pseudo<(outs GR16:$d), (ins HL16:$p),
                         [(set GR16:$d, (load HL16:$p))]>;
  def LD16rf : Z80Pseudo<(outs GR16:$d), (ins memix:$m),
                         [(set GR16:$d, (load addr_ix:$m))]>;
}

let mayStore = 1 in {
  def ST8pr : Z80Inst<1, (outs), (ins HL16:$p, GR8:$s), "ld\t(hl), $s",
                      [(store GR8:$s, HL16:$p)]> {
    bits<3> s;
    let Inst{7-3} = 0b01110;
    let Inst{2-0} = s;
  }
  def ST8fr : Z80Inst<3, (outs), (ins memix:$m, GR8:$s), "ld\t$m, $s",
                      [(store GR8:$s, addr_ix:$m)]> {
    bits<8> m;
    bits<3> s;
    let Inst{23-16} = 0xDD;
    let Inst{15-11} = 0b01110;
    let Inst{10-8} = s;
    let Inst{7-0} = m;
  }
  def ST8ar : Z80Inst<3, (outs), (ins imm16op:$a, ACC8:$s), "ld\t($a), a"> {
    bits<16> a;
    let Inst{23-16} = 0x32;
    let Inst{15-0} = a;
  }
  def ST16ar : Z80Inst<3, (outs), (ins imm16op:$a, HL16:$s), "ld\t($a), hl"> {
    bits<16> a;
    let Inst{23-16} = 0x22;
    let Inst{15-0} = a;
  }
  def ST16pr : Z80Pseudo<(outs), (ins HL16:$p, NOHL16:$s),
                         [(store NOHL16:$s, HL16:$p)]>;
  def ST16fr : Z80Pseudo<(outs), (ins memix:$m, GR16:$s),
                         [(store GR16:$s, addr_ix:$m)]>;
}

def : Pat<(i8 (load (Z80wrapper tglobaladdr:$a))), (LD8ra tglobaladdr:$a)>;
def : Pat<(i16 (load (Z80wrapper tglobaladdr:$a))), (LD16ra tglobaladdr:$a)>;
def : Pat<(store ACC8:$s, (Z80wrapper tglobaladdr:$a)), (ST8ar tglobaladdr:$a, ACC8:$s)>;
def : Pat<(store HL16:$s, (Z80wrapper tglobaladdr:$a)), (ST16ar tglobaladdr:$a, HL16:$s)>;

def : Pat<(i8 (load (i16 imm:$a))), (LD8ra imm:$a)>;
def : Pat<(store ACC8:$s, (i16 imm:$a)), (ST8ar imm:$a, ACC8:$s)>;

// Address space 1 is the Z80's ports. The port is the pointer's value: `in`/`out` with the
// number in the instruction when it is known, through C when it is not.
def port_load : PatFrag<(ops node:$p), (load node:$p),
                        [{ return cast<LoadSDNode>(N)->getAddressSpace() == 1; }]>;
def port_store : PatFrag<(ops node:$v, node:$p), (store node:$v, node:$p),
                         [{ return cast<StoreSDNode>(N)->getAddressSpace() == 1; }]>;
def port_imm : ImmLeaf<i16, [{ return Imm >= 0 && Imm < 256; }]>;
def port8 : SDNodeXForm<imm, [{
  return CurDAG->getTargetConstant(N->getZExtValue(), SDLoc(N), MVT::i8);
}]>;

let hasSideEffects = 1 in {
  def INAn : Z80Inst<2, (outs ACC8:$d), (ins i8imm:$n), "in\ta, ($n)"> {
    bits<8> n;
    let Inst{15-8} = 0xDB;
    let Inst{7-0} = n;
  }
  def OUTnA : Z80Inst<2, (outs), (ins i8imm:$n, ACC8:$s), "out\t($n), a"> {
    bits<8> n;
    let Inst{15-8} = 0xD3;
    let Inst{7-0} = n;
  }
  let Defs = [F] in
  def INrC : Z80Inst<2, (outs GR8:$d), (ins BC16:$p), "in\t$d, (c)"> {
    bits<3> d;
    let Inst{15-8} = 0xED;
    let Inst{7-6} = 0b01;
    let Inst{5-3} = d;
    let Inst{2-0} = 0b000;
  }
  def OUTCr : Z80Inst<2, (outs), (ins BC16:$p, GR8:$s), "out\t(c), $s"> {
    bits<3> s;
    let Inst{15-8} = 0xED;
    let Inst{7-6} = 0b01;
    let Inst{5-3} = s;
    let Inst{2-0} = 0b001;
  }
}

let AddedComplexity = 100 in {
  def : Pat<(i8 (port_load port_imm:$n)), (INAn (port8 port_imm:$n))>;
  def : Pat<(port_store ACC8:$s, port_imm:$n), (OUTnA (port8 port_imm:$n), ACC8:$s)>;
}
let AddedComplexity = 90 in {
  def : Pat<(i8 (port_load BC16:$p)), (INrC BC16:$p)>;
  def : Pat<(port_store GR8:$s, BC16:$p), (OUTCr BC16:$p, GR8:$s)>;
}

// A stack object's address: selected from a frame index by Z80DAGToDAGISel.
let Defs = [F], isReMaterializable = 1 in
def LEA16 : Z80Pseudo<(outs HL16:$d), (ins memix:$m)>;

//===----------------------------------------------------------------------===//
// 8-bit arithmetic, through A
//===----------------------------------------------------------------------===//

multiclass ALU8<bits<3> op, string name, SDPatternOperator node, bit commutes> {
  let Defs = [F], Constraints = "$d = $a" in {
    let isCommutable = 0 in
    def r : Z80Inst<1, (outs ACC8:$d), (ins ACC8:$a, GR8:$r), !strconcat(name, "\t$r"),
                    [(set ACC8:$d, (node ACC8:$a, GR8:$r))]> {
      bits<3> r;
      let Inst{7-6} = 0b10;
      let Inst{5-3} = op;
      let Inst{2-0} = r;
    }
    def i : Z80Inst<2, (outs ACC8:$d), (ins ACC8:$a, i8imm:$n), !strconcat(name, "\t$n"),
                    [(set ACC8:$d, (node ACC8:$a, imm:$n))]> {
      bits<8> n;
      let Inst{15-14} = 0b11;
      let Inst{13-11} = op;
      let Inst{10-8} = 0b110;
      let Inst{7-0} = n;
    }
  }
}

defm ADD8 : ALU8<0b000, "add\ta,", add, 1>;
defm SUB8 : ALU8<0b010, "sub", sub, 0>;
defm AND8 : ALU8<0b100, "and", and, 1>;
defm XOR8 : ALU8<0b101, "xor", xor, 1>;
defm OR8 : ALU8<0b110, "or", or, 1>;

let Defs = [F] in {
  def CP8r : Z80Inst<1, (outs), (ins ACC8:$a, GR8:$r), "cp\t$r",
                     [(Z80cmp ACC8:$a, GR8:$r)]> {
    bits<3> r;
    let Inst{7-3} = 0b10111;
    let Inst{2-0} = r;
  }
  def CP8i : Z80Inst<2, (outs), (ins ACC8:$a, i8imm:$n), "cp\t$n",
                     [(Z80cmp ACC8:$a, imm:$n)]> {
    bits<8> n;
    let Inst{15-8} = 0xFE;
    let Inst{7-0} = n;
  }
  // Compares HL with a pair and leaves HL as it was: `or a; sbc hl, rr; add hl, rr`. The add
  // changes neither Z nor, since it undoes the subtraction exactly, the carry.
  def CP16 : Z80Pseudo<(outs), (ins HL16:$a, NOHL16:$b), [(Z80cmp HL16:$a, NOHL16:$b)]>;
}

let Defs = [F] in {
  def BIT7r : Z80Inst<2, (outs), (ins GR8:$r), "bit\t7, $r"> {
    bits<3> r;
    let Inst{15-3} = 0b1100101101111;
    let Inst{2-0} = r;
  }
  // `ld a, hi; or lo`.
  let Defs = [A, F] in
  def TESTZ16 : Z80Pseudo<(outs), (ins GR16:$r), [(Z80testz GR16:$r)]>;
  def TESTZ8 : Z80Pseudo<(outs), (ins ACC8:$r), [(Z80testz ACC8:$r)]>;
  def TESTS16 : Z80Pseudo<(outs), (ins GR16:$r), [(Z80tests GR16:$r)]>;
}
def : Pat<(Z80tests GR8:$r), (BIT7r GR8:$r)>;

// CB shifts by one, of any register; the wider ones are built from them.
class Shift8<bits<5> op, string name, dag pat>
    : Z80Inst<2, (outs GR8:$d), (ins GR8:$s), !strconcat(name, "\t$d"), [pat]> {
  bits<3> d;
  let Constraints = "$d = $s";
  let Defs = [F];
  let Inst{15-8} = 0xCB;
  let Inst{7-3} = op;
  let Inst{2-0} = d;
}
def SLA8 : Shift8<0b00100, "sla", (set GR8:$d, (Z80lsl GR8:$s))>;
def SRA8 : Shift8<0b00101, "sra", (set GR8:$d, (Z80asr GR8:$s))>;
def SRL8 : Shift8<0b00111, "srl", (set GR8:$d, (Z80lsr GR8:$s))>;

let Defs = [F], Uses = [F], Constraints = "$d = $s" in {
  def RL8 : Z80Inst<2, (outs GR8:$d), (ins GR8:$s), "rl\t$d"> {
    bits<3> d;
    let Inst{15-3} = 0b1100101100010;
    let Inst{2-0} = d;
  }
  def RR8 : Z80Inst<2, (outs GR8:$d), (ins GR8:$s), "rr\t$d"> {
    bits<3> d;
    let Inst{15-3} = 0b1100101100011;
    let Inst{2-0} = d;
  }
}

// A shift by eight or more starts from the high byte.
def : Pat<(i8 (Z80hibyte GR16:$s)), (EXTRACT_SUBREG GR16:$s, sub_hi)>;
let Constraints = "$d = $s" in
def BYTEUP16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$s), [(set GR16:$d, (Z80byteup GR16:$s))]>;

// What pseudos expand to.
let Defs = [A, F], Uses = [A] in
def RLCA : Z80Inst<1, (outs), (ins), "rlca"> {
  let Inst{7-0} = 0x07;
}
let Defs = [A, F], Uses = [A, F] in
def SBCAA : Z80Inst<1, (outs), (ins), "sbc\ta, a"> {
  let Inst{7-0} = 0x9F;
}
let Defs = [F], Uses = [A] in
def ORAA : Z80Inst<1, (outs), (ins), "or\ta"> {
  let Inst{7-0} = 0xB7;
}

//===----------------------------------------------------------------------===//
// 16-bit arithmetic, through HL
//===----------------------------------------------------------------------===//

let Defs = [F], Constraints = "$d = $a" in {
  def ADD16 : Z80Inst<1, (outs HL16:$d), (ins HL16:$a, GR16:$r), "add\thl, $r",
                      [(set HL16:$d, (add HL16:$a, GR16:$r))]> {
    bits<2> r;
    let Inst{7-6} = 0b00;
    let Inst{5-4} = r;
    let Inst{3-0} = 0b1001;
  }
  let Uses = [F] in {
    def ADC16 : Z80Inst<2, (outs HL16:$d), (ins HL16:$a, GR16:$r), "adc\thl, $r",
                        [(set HL16:$d, (adde HL16:$a, GR16:$r))]> {
      bits<2> r;
      let Inst{15-8} = 0xED;
      let Inst{7-6} = 0b01;
      let Inst{5-4} = r;
      let Inst{3-0} = 0b1010;
    }
    def SBC16 : Z80Inst<2, (outs HL16:$d), (ins HL16:$a, GR16:$r), "sbc\thl, $r",
                        [(set HL16:$d, (sube HL16:$a, GR16:$r))]> {
      bits<2> r;
      let Inst{15-8} = 0xED;
      let Inst{7-6} = 0b01;
      let Inst{5-4} = r;
      let Inst{3-0} = 0b0010;
    }
  }
  // `or a; sbc hl, rr`.
  def SUB16 : Z80Pseudo<(outs HL16:$d), (ins HL16:$a, GR16:$r),
                        [(set HL16:$d, (sub HL16:$a, GR16:$r))]>;
}
def : Pat<(addc HL16:$a, GR16:$r), (ADD16 HL16:$a, GR16:$r)>;
def : Pat<(subc HL16:$a, GR16:$r), (SUB16 HL16:$a, GR16:$r)>;

// A byte at a time through A.
let Defs = [A, F], Constraints = "$d = $a" in {
  def AND16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$a, GR16:$b),
                        [(set GR16:$d, (and GR16:$a, GR16:$b))]>;
  def OR16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$a, GR16:$b),
                       [(set GR16:$d, (or GR16:$a, GR16:$b))]>;
  def XOR16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$a, GR16:$b),
                        [(set GR16:$d, (xor GR16:$a, GR16:$b))]>;
}

let Defs = [F], Constraints = "$d = $s" in {
  def LSL16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$s), [(set GR16:$d, (Z80lsl GR16:$s))]>;
  def LSR16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$s), [(set GR16:$d, (Z80lsr GR16:$s))]>;
  def ASR16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$s), [(set GR16:$d, (Z80asr GR16:$s))]>;
}

// No flags change: spills and copies may sit between a compare and its jump. And six T-states
// against twenty-one for loading a small constant and adding it, of any pair.
let Constraints = "$d = $s" in {
  def INC16 : Z80Inst<1, (outs GR16:$d), (ins GR16:$s), "inc\t$d"> {
    bits<2> d;
    let Inst{7-6} = 0b00;
    let Inst{5-4} = d;
    let Inst{3-0} = 0b0011;
  }
  def DEC16 : Z80Inst<1, (outs GR16:$d), (ins GR16:$s), "dec\t$d"> {
    bits<2> d;
    let Inst{7-6} = 0b00;
    let Inst{5-4} = d;
    let Inst{3-0} = 0b1011;
  }
}

def : Pat<(add GR16:$s, 1), (INC16 GR16:$s)>;
def : Pat<(add GR16:$s, 2), (INC16 (INC16 GR16:$s))>;
def : Pat<(add GR16:$s, 3), (INC16 (INC16 (INC16 GR16:$s)))>;
def : Pat<(add GR16:$s, -1), (DEC16 GR16:$s)>;
def : Pat<(add GR16:$s, -2), (DEC16 (DEC16 GR16:$s))>;
def : Pat<(add GR16:$s, -3), (DEC16 (DEC16 (DEC16 GR16:$s)))>;

//===----------------------------------------------------------------------===//
// Checked arithmetic
//===----------------------------------------------------------------------===//

// `or a; adc hl, rr`: unlike `add hl, rr`, this sets the overflow flag.
let Defs = [A, F], Constraints = "$d = $a" in
def SADD16 : Z80Pseudo<(outs HL16:$d), (ins HL16:$a, GR16:$r)>;

// A flag as a 0 or 1: the printer writes `ld r, 0; jp !cc, past; inc r; past:`.
let Uses = [F], Defs = [F] in
def SETCC8 : Z80Pseudo<(outs GR8:$d), (ins ccop:$cc)>;
let Defs = [F] in
def INC8 : Z80Inst<1, (outs GR8:$d), (ins GR8:$s), "inc\t$d"> {
  bits<3> d;
  let Constraints = "$d = $s";
  let Inst{7-6} = 0b00;
  let Inst{5-3} = d;
  let Inst{2-0} = 0b100;
}

let Defs = [F], Constraints = "@earlyclobber $o, $d = $a" in {
  def SADDO8 : Z80Pseudo<(outs ACC8:$d, GR8:$o), (ins ACC8:$a, GR8:$r),
                         [(set ACC8:$d, GR8:$o, (Z80saddo ACC8:$a, GR8:$r))]>;
  def SSUBO8 : Z80Pseudo<(outs ACC8:$d, GR8:$o), (ins ACC8:$a, GR8:$r),
                         [(set ACC8:$d, GR8:$o, (Z80ssubo ACC8:$a, GR8:$r))]>;
  def UADDO8 : Z80Pseudo<(outs ACC8:$d, GR8:$o), (ins ACC8:$a, GR8:$r),
                         [(set ACC8:$d, GR8:$o, (Z80uaddo ACC8:$a, GR8:$r))]>;
  def USUBO8 : Z80Pseudo<(outs ACC8:$d, GR8:$o), (ins ACC8:$a, GR8:$r),
                         [(set ACC8:$d, GR8:$o, (Z80usubo ACC8:$a, GR8:$r))]>;
  let Defs = [A, F] in
  def SADDO16 : Z80Pseudo<(outs HL16:$d, GR8:$o), (ins HL16:$a, GR16:$r),
                          [(set HL16:$d, GR8:$o, (Z80saddo HL16:$a, GR16:$r))]>;
  def SSUBO16 : Z80Pseudo<(outs HL16:$d, GR8:$o), (ins HL16:$a, GR16:$r),
                          [(set HL16:$d, GR8:$o, (Z80ssubo HL16:$a, GR16:$r))]>;
  def UADDO16 : Z80Pseudo<(outs HL16:$d, GR8:$o), (ins HL16:$a, GR16:$r),
                          [(set HL16:$d, GR8:$o, (Z80uaddo HL16:$a, GR16:$r))]>;
  def USUBO16 : Z80Pseudo<(outs HL16:$d, GR8:$o), (ins HL16:$a, GR16:$r),
                          [(set HL16:$d, GR8:$o, (Z80usubo HL16:$a, GR16:$r))]>;
}

//===----------------------------------------------------------------------===//
// Extensions
//===----------------------------------------------------------------------===//

def ZEXT16 : Z80Pseudo<(outs GR16:$d), (ins GR8:$s), [(set GR16:$d, (zext GR8:$s))]>;
let Defs = [A, F] in
def SEXT16 : Z80Pseudo<(outs GR16:$d), (ins GR8:$s), [(set GR16:$d, (sext GR8:$s))]>;
def : Pat<(i8 (trunc GR16:$s)), (EXTRACT_SUBREG GR16:$s, sub_lo)>;
def : Pat<(i16 (anyext GR8:$s)),
          (INSERT_SUBREG (i16 (IMPLICIT_DEF)), GR8:$s, sub_lo)>;

//===----------------------------------------------------------------------===//
// Control flow
//===----------------------------------------------------------------------===//

let isBranch = 1, isTerminator = 1 in {
  let isBarrier = 1 in
  def JP : Z80Inst<3, (outs), (ins brtarget:$t), "jp\t$t", [(br bb:$t)]> {
    bits<16> t;
    let Inst{23-16} = 0xC3;
    let Inst{15-0} = t;
  }
  let Uses = [F] in
  def JPcc : Z80Inst<3, (outs), (ins brtarget:$t, ccop:$cc), "jp\t$cc, $t",
                     [(Z80brcond bb:$t, timm:$cc)]> {
    bits<16> t;
    bits<3> cc;
    let Inst{23-22} = 0b11;
    let Inst{21-19} = cc;
    let Inst{18-16} = 0b010;
    let Inst{15-0} = t;
  }
}

let usesCustomInserter = 1, Uses = [F] in {
  def SELECT8 : Z80Pseudo<(outs GR8:$d), (ins GR8:$t, GR8:$f, i8imm:$cc),
                          [(set GR8:$d, (Z80select GR8:$t, GR8:$f, timm:$cc))]>;
  def SELECT16 : Z80Pseudo<(outs GR16:$d), (ins GR16:$t, GR16:$f, i8imm:$cc),
                           [(set GR16:$d, (Z80select GR16:$t, GR16:$f, timm:$cc))]>;
}

let isCall = 1, Uses = [SP], Defs = [SP, A, F, BC, DE, HL] in
def CALL : Z80Inst<3, (outs), (ins imm16op:$a, variable_ops), "call\t$a"> {
  bits<16> a;
  let Inst{23-16} = 0xCD;
  let Inst{15-0} = a;
}
def : Pat<(Z80call tglobaladdr:$a), (CALL tglobaladdr:$a)>;
def : Pat<(Z80call texternalsym:$a), (CALL texternalsym:$a)>;

let isReturn = 1, isTerminator = 1, isBarrier = 1 in
def RET : Z80Inst<1, (outs), (ins variable_ops), "ret", [(Z80ret)]> {
  let Inst{7-0} = 0xC9;
}

let Defs = [SP], Uses = [SP] in {
  def ADJCALLSTACKDOWN : Z80Pseudo<(outs), (ins i16imm:$amt, i16imm:$amt2),
                                   [(callseq_start timm:$amt, timm:$amt2)]>;
  def ADJCALLSTACKUP : Z80Pseudo<(outs), (ins i16imm:$amt, i16imm:$amt2),
                                 [(callseq_end timm:$amt, timm:$amt2)]>;
}

//===----------------------------------------------------------------------===//
// The frame: IX is the stack pointer's copy once the prologue has run
//===----------------------------------------------------------------------===//

let Uses = [SP, IX], Defs = [SP], mayStore = 1 in
def PUSHIX : Z80Inst<2, (outs), (ins), "push\tix"> {
  let Inst{15-0} = 0xDDE5;
}
let Uses = [SP], Defs = [SP, IX], mayLoad = 1 in
def POPIX : Z80Inst<2, (outs), (ins), "pop\tix"> {
  let Inst{15-0} = 0xDDE1;
}
let Defs = [IX] in
def LDIXi : Z80Inst<4, (outs), (ins imm16op:$n), "ld\tix, $n"> {
  bits<16> n;
  let Inst{31-16} = 0xDD21;
  let Inst{15-0} = n;
}
let Defs = [IX, F], Uses = [IX, SP] in
def ADDIXSP : Z80Inst<2, (outs), (ins), "add\tix, sp"> {
  let Inst{15-0} = 0xDD39;
}
let Defs = [SP], Uses = [IX] in
def LDSPIX : Z80Inst<2, (outs), (ins), "ld\tsp, ix"> {
  let Inst{15-0} = 0xDDF9;
}
let Uses = [SP], Defs = [SP] in {
  let mayStore = 1 in {
    def PUSH16 : Z80Inst<1, (outs), (ins GR16:$r), "push\t$r"> {
      bits<2> r;
      let Inst{7-6} = 0b11;
      let Inst{5-4} = r;
      let Inst{3-0} = 0b0101;
    }
    let Uses = [SP, A, F] in
    def PUSHAF : Z80Inst<1, (outs), (ins), "push\taf"> {
      let Inst{7-0} = 0xF5;
    }
  }
  let mayLoad = 1 in {
    def POP16 : Z80Inst<1, (outs GR16:$r), (ins), "pop\t$r"> {
      bits<2> r;
      let Inst{7-6} = 0b11;
      let Inst{5-4} = r;
      let Inst{3-0} = 0b0001;
    }
    let Defs = [SP, A, F] in
    def POPAF : Z80Inst<1, (outs), (ins), "pop\taf"> {
      let Inst{7-0} = 0xF1;
    }
  }
}
let Defs = [IX, F], Uses = [IX, DE] in
def ADDIXDE : Z80Inst<2, (outs), (ins), "add\tix, de"> {
  let Inst{15-0} = 0xDD19;
}
let Defs = [F], Uses = [SP], Constraints = "$d = $a" in
def ADDHLSP : Z80Inst<1, (outs HL16:$d), (ins HL16:$a), "add\thl, sp"> {
  let Inst{7-0} = 0x39;
}
