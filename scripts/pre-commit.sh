#!/bin/bash
# Pre-commit hook for Go formatting check
# 提交前格式化检查

files=$(gofmt -l .)
if [ -n "$files" ]; then
    echo "Error: These files need formatting:"
    echo "$files"
    echo ""
    echo "Run 'gofmt -w .' to fix formatting."
    exit 1
fi

# Run go vet
if ! go vet ./...; then
    echo "Error: go vet failed"
    exit 1
fi

echo "Pre-commit checks passed!"
exit 0
