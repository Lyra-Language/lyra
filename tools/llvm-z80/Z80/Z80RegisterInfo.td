//===-- Z80RegisterInfo.td - Z80 registers -----------------------*- tablegen -*-===//
//
// The 8-bit registers carry their `r` field as encoding and the pairs their `rr` field.
//
//===----------------------------------------------------------------------===//

class Z80Reg<string n, bits<16> enc> : Register<n> {
  let Namespace = "Z80";
  let HWEncoding = enc;
}

let Namespace = "Z80" in {
  def sub_lo : SubRegIndex<8>;
  def sub_hi : SubRegIndex<8, 8>;
}

class Z80Pair<string n, bits<16> enc, Register lo, Register hi> : Z80Reg<n, enc> {
  let SubRegs = [lo, hi];
  let SubRegIndices = [sub_lo, sub_hi];
  let CoveredBySubRegs = 1;
}

def B : Z80Reg<"b", 0>;
def C : Z80Reg<"c", 1>;
def D : Z80Reg<"d", 2>;
def E : Z80Reg<"e", 3>;
def H : Z80Reg<"h", 4>;
def L : Z80Reg<"l", 5>;
def A : Z80Reg<"a", 7>;

def BC : Z80Pair<"bc", 0, C, B>;
def DE : Z80Pair<"de", 1, E, D>;
def HL : Z80Pair<"hl", 2, L, H>;

def SP : Z80Reg<"sp", 3>;
def IX : Z80Reg<"ix", 4>;
def IY : Z80Reg<"iy", 5>;
def F : Z80Reg<"f", 0>;

def GR8 : RegisterClass<"Z80", [i8], 8, (add A, L, H, E, D, C, B)>;
def ACC8 : RegisterClass<"Z80", [i8], 8, (add A)>;

def GR16 : RegisterClass<"Z80", [i16], 8, (add HL, DE, BC)>;
def HL16 : RegisterClass<"Z80", [i16], 8, (add HL)>;
def BC16 : RegisterClass<"Z80", [i16], 8, (add BC)>;
def NOHL16 : RegisterClass<"Z80", [i16], 8, (add DE, BC)>;
def IX16 : RegisterClass<"Z80", [i16], 8, (add IX)>;
def SP16 : RegisterClass<"Z80", [i16], 8, (add SP)>;

def FLAGS : RegisterClass<"Z80", [i8], 8, (add F)> {
  let CopyCost = -1;
  let isAllocatable = 0;
}
