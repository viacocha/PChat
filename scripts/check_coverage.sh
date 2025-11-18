#!/bin/bash

# 测试覆盖率检查脚本
# 用法: ./scripts/check_coverage.sh [阈值]

set -e

THRESHOLD=${1:-50.0}
COVERAGE_FILE="coverage.out"

echo "🔍 检查测试覆盖率..."
echo "阈值: ${THRESHOLD}%"
echo ""

# 生成覆盖率报告
if [ ! -f "$COVERAGE_FILE" ]; then
    echo "📊 生成覆盖率报告..."
    go test ./... -coverprofile="$COVERAGE_FILE"
fi

# 显示覆盖率摘要
echo "📈 覆盖率摘要:"
echo "=================="
go tool cover -func="$COVERAGE_FILE" | grep -E "^cmd/pchat|^internal/discovery|^cmd/registry|^internal/crypto|^internal/registry|^total:" | while read line; do
    echo "$line"
done
echo ""

# 检查总体覆盖率
TOTAL_COVERAGE=$(go tool cover -func="$COVERAGE_FILE" | grep "^total:" | awk '{print $3}' | sed 's/%//')

if awk "BEGIN {exit !($TOTAL_COVERAGE < $THRESHOLD)}"; then
    echo "❌ 总体覆盖率 ($TOTAL_COVERAGE%) 低于阈值 ($THRESHOLD%)"
    echo ""
    echo "需要改进的模块:"
    go tool cover -func="$COVERAGE_FILE" | grep -E "^cmd/pchat|^internal/discovery" | awk -v threshold="$THRESHOLD" '{
        coverage = $3
        gsub(/%/, "", coverage)
        if (coverage < threshold) {
            printf "  - %s: %s (目标: >%.1f%%)\n", $1, $3, threshold
        }
    }'
    exit 1
else
    echo "✅ 总体覆盖率 ($TOTAL_COVERAGE%) 达到阈值 ($THRESHOLD%)"
fi

# 检查各模块覆盖率
echo ""
echo "📋 模块覆盖率检查:"
echo "=================="

check_module() {
    local module=$1
    local threshold=$2
    local coverage=$(go tool cover -func="$COVERAGE_FILE" | grep "^$module" | awk '{print $3}' | sed 's/%//')
    
    if [ -z "$coverage" ]; then
        echo "  ⚠️  $module: 未找到覆盖率数据"
        return
    fi
    
    if awk "BEGIN {exit !($coverage < $threshold)}"; then
        echo "  ❌ $module: ${coverage}% (目标: >${threshold}%)"
        return 1
    else
        echo "  ✅ $module: ${coverage}% (目标: >${threshold}%)"
        return 0
    fi
}

FAILED=0
check_module "cmd/pchat" "50.0" || FAILED=1
check_module "internal/discovery" "40.0" || FAILED=1
check_module "cmd/registry" "30.0" || FAILED=1
check_module "internal/crypto" "70.0" || FAILED=1
check_module "internal/registry" "30.0" || FAILED=1

echo ""
if [ $FAILED -eq 1 ]; then
    echo "❌ 部分模块未达到覆盖率阈值"
    exit 1
else
    echo "✅ 所有模块都达到覆盖率阈值"
fi

