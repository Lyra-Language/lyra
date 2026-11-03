add_llvm_component_library(LLVMZ80Desc
  Z80AsmBackend.cpp
  Z80InstPrinter.cpp
  Z80MCCodeEmitter.cpp
  Z80MCTargetDesc.cpp

  LINK_COMPONENTS
  MC
  Support
  TargetParser
  Z80Info

  ADD_TO_COMPONENT
  Z80
)

add_dependencies(LLVMZ80Desc Z80CommonTableGen)
