add_llvm_component_group(Z80)

set(LLVM_TARGET_DEFINITIONS Z80.td)

tablegen(LLVM Z80GenAsmWriter.inc -gen-asm-writer)
tablegen(LLVM Z80GenCallingConv.inc -gen-callingconv)
tablegen(LLVM Z80GenDAGISel.inc -gen-dag-isel)
tablegen(LLVM Z80GenInstrInfo.inc -gen-instr-info)
tablegen(LLVM Z80GenMCCodeEmitter.inc -gen-emitter)
tablegen(LLVM Z80GenRegisterInfo.inc -gen-register-info)
tablegen(LLVM Z80GenSDNodeInfo.inc -gen-sd-node-info)
tablegen(LLVM Z80GenSubtargetInfo.inc -gen-subtarget)

add_public_tablegen_target(Z80CommonTableGen)

add_llvm_target(Z80CodeGen
  Z80AsmPrinter.cpp
  Z80ConstrainedCopies.cpp
  Z80FrameLowering.cpp
  Z80InstrInfo.cpp
  Z80ISelDAGToDAG.cpp
  Z80ISelLowering.cpp
  Z80RegisterInfo.cpp
  Z80SelectionDAGInfo.cpp
  Z80Subtarget.cpp
  Z80TargetMachine.cpp

  DEPENDS
  intrinsics_gen

  LINK_COMPONENTS
  Analysis
  AsmPrinter
  CodeGen
  CodeGenTypes
  Core
  MC
  SelectionDAG
  Support
  Target
  TargetParser
  Z80Desc
  Z80Info

  ADD_TO_COMPONENT
  Z80
  )

add_subdirectory(MCTargetDesc)
add_subdirectory(TargetInfo)
