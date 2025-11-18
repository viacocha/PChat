#!/bin/bash

# 测试覆盖率监控脚本
# 用于持续监控测试覆盖率并生成报告

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 覆盖率阈值
TARGET_OVERALL=50
TARGET_PCHAT=50
TARGET_DISCOVERY=40
TARGET_REGISTRY=30
TARGET_CRYPTO=70

# 覆盖率文件
COVERAGE_FILE="coverage.out"
COVERAGE_HTML="coverage.html"
COVERAGE_SUMMARY="coverage_summary.txt"
COVERAGE_HISTORY="coverage_history.txt"

echo "=========================================="
echo "PChat 测试覆盖率监控"
echo "=========================================="
echo ""

# 生成覆盖率报告（设置超时避免长时间运行）
echo "📊 生成覆盖率报告..."
timeout 300 go test ./... -coverprofile=$COVERAGE_FILE -covermode=atomic -timeout=5m || {
    echo -e "${YELLOW}⚠️  部分测试可能超时或失败，继续处理已生成的覆盖率数据...${NC}"
}

if [ ! -f "$COVERAGE_FILE" ]; then
    echo -e "${RED}❌ 覆盖率文件生成失败${NC}"
    exit 1
fi

# 生成 HTML 报告
echo "📄 生成 HTML 覆盖率报告..."
go tool cover -html=$COVERAGE_FILE -o $COVERAGE_HTML

# 生成文本摘要
echo "📋 生成覆盖率摘要..."
go tool cover -func=$COVERAGE_FILE > $COVERAGE_SUMMARY

# 提取各模块覆盖率
echo ""
echo "=========================================="
echo "模块覆盖率"
echo "=========================================="

# 提取总体覆盖率
TOTAL_COVERAGE=$(grep "^total:" $COVERAGE_SUMMARY | awk '{print $3}' | sed 's/%//')
echo -e "总体覆盖率: ${GREEN}${TOTAL_COVERAGE}%${NC} (目标: ${TARGET_OVERALL}%)"

# 提取各模块覆盖率
PCHAT_COVERAGE=$(grep "^cmd/pchat" $COVERAGE_SUMMARY | head -1 | awk '{print $3}' | sed 's/%//' || echo "0")
DISCOVERY_COVERAGE=$(grep "^internal/discovery" $COVERAGE_SUMMARY | head -1 | awk '{print $3}' | sed 's/%//' || echo "0")
REGISTRY_COVERAGE=$(grep "^cmd/registry" $COVERAGE_SUMMARY | head -1 | awk '{print $3}' | sed 's/%//' || echo "0")
CRYPTO_COVERAGE=$(grep "^internal/crypto" $COVERAGE_SUMMARY | head -1 | awk '{print $3}' | sed 's/%//' || echo "0")

# 显示各模块覆盖率
echo -e "cmd/pchat:      ${PCHAT_COVERAGE}% (目标: ${TARGET_PCHAT}%)"
echo -e "internal/discovery: ${DISCOVERY_COVERAGE}% (目标: ${TARGET_DISCOVERY}%)"
echo -e "cmd/registry:   ${REGISTRY_COVERAGE}% (目标: ${TARGET_REGISTRY}%)"
echo -e "internal/crypto: ${CRYPTO_COVERAGE}% (目标: ${TARGET_CRYPTO}%)"

# 检查覆盖率阈值
echo ""
echo "=========================================="
echo "覆盖率检查"
echo "=========================================="

FAILED=0

# 检查总体覆盖率（使用 awk 进行浮点数比较）
TOTAL_CHECK=$(echo "$TOTAL_COVERAGE $TARGET_OVERALL" | awk '{if ($1 < $2) print "FAIL"; else print "PASS"}')
if [ "$TOTAL_CHECK" = "FAIL" ]; then
    echo -e "${RED}❌ 总体覆盖率未达标: ${TOTAL_COVERAGE}% < ${TARGET_OVERALL}%${NC}"
    FAILED=1
else
    echo -e "${GREEN}✅ 总体覆盖率达标: ${TOTAL_COVERAGE}% >= ${TARGET_OVERALL}%${NC}"
fi

# 检查 cmd/pchat 覆盖率
PCHAT_CHECK=$(echo "$PCHAT_COVERAGE $TARGET_PCHAT" | awk '{if ($1 < $2) print "FAIL"; else print "PASS"}')
if [ "$PCHAT_CHECK" = "FAIL" ]; then
    echo -e "${YELLOW}⚠️  cmd/pchat 覆盖率未达标: ${PCHAT_COVERAGE}% < ${TARGET_PCHAT}%${NC}"
else
    echo -e "${GREEN}✅ cmd/pchat 覆盖率达标: ${PCHAT_COVERAGE}% >= ${TARGET_PCHAT}%${NC}"
fi

# 检查 internal/discovery 覆盖率
DISCOVERY_CHECK=$(echo "$DISCOVERY_COVERAGE $TARGET_DISCOVERY" | awk '{if ($1 < $2) print "FAIL"; else print "PASS"}')
if [ "$DISCOVERY_CHECK" = "FAIL" ]; then
    echo -e "${YELLOW}⚠️  internal/discovery 覆盖率未达标: ${DISCOVERY_COVERAGE}% < ${TARGET_DISCOVERY}%${NC}"
else
    echo -e "${GREEN}✅ internal/discovery 覆盖率达标: ${DISCOVERY_COVERAGE}% >= ${TARGET_DISCOVERY}%${NC}"
fi

# 检查 cmd/registry 覆盖率
REGISTRY_CHECK=$(echo "$REGISTRY_COVERAGE $TARGET_REGISTRY" | awk '{if ($1 < $2) print "FAIL"; else print "PASS"}')
if [ "$REGISTRY_CHECK" = "FAIL" ]; then
    echo -e "${YELLOW}⚠️  cmd/registry 覆盖率未达标: ${REGISTRY_COVERAGE}% < ${TARGET_REGISTRY}%${NC}"
else
    echo -e "${GREEN}✅ cmd/registry 覆盖率达标: ${REGISTRY_COVERAGE}% >= ${TARGET_REGISTRY}%${NC}"
fi

# 检查 internal/crypto 覆盖率
CRYPTO_CHECK=$(echo "$CRYPTO_COVERAGE $TARGET_CRYPTO" | awk '{if ($1 < $2) print "FAIL"; else print "PASS"}')
if [ "$CRYPTO_CHECK" = "FAIL" ]; then
    echo -e "${YELLOW}⚠️  internal/crypto 覆盖率未达标: ${CRYPTO_COVERAGE}% < ${TARGET_CRYPTO}%${NC}"
else
    echo -e "${GREEN}✅ internal/crypto 覆盖率达标: ${CRYPTO_COVERAGE}% >= ${TARGET_CRYPTO}%${NC}"
fi

# 记录覆盖率历史
TIMESTAMP=$(date +"%Y-%m-%d %H:%M:%S")
echo "$TIMESTAMP,$TOTAL_COVERAGE,$PCHAT_COVERAGE,$DISCOVERY_COVERAGE,$REGISTRY_COVERAGE,$CRYPTO_COVERAGE" >> $COVERAGE_HISTORY

echo ""
echo "=========================================="
echo "覆盖率报告已生成"
echo "=========================================="
echo "HTML 报告: $COVERAGE_HTML"
echo "文本摘要: $COVERAGE_SUMMARY"
echo "历史记录: $COVERAGE_HISTORY"
echo ""

if [ $FAILED -eq 1 ]; then
    echo -e "${RED}❌ 覆盖率检查失败${NC}"
    exit 1
else
    echo -e "${GREEN}✅ 覆盖率检查通过${NC}"
    exit 0
fi

