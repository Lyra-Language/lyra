#include "Z80SelectionDAGInfo.h"

#define GET_SDNODE_DESC
#include "Z80GenSDNodeInfo.inc"

using namespace llvm;

Z80SelectionDAGInfo::Z80SelectionDAGInfo()
    : SelectionDAGGenTargetInfo(Z80GenSDNodeInfo) {}

Z80SelectionDAGInfo::~Z80SelectionDAGInfo() = default;
