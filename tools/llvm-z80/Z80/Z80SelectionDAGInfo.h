#ifndef LLVM_LIB_TARGET_Z80_Z80SELECTIONDAGINFO_H
#define LLVM_LIB_TARGET_Z80_Z80SELECTIONDAGINFO_H

#include "llvm/CodeGen/SelectionDAGTargetInfo.h"

#define GET_SDNODE_ENUM
#include "Z80GenSDNodeInfo.inc"

namespace llvm {
class Z80SelectionDAGInfo : public SelectionDAGGenTargetInfo {
public:
  Z80SelectionDAGInfo();
  ~Z80SelectionDAGInfo() override;
};
} // namespace llvm

#endif
