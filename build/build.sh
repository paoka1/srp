#!/bin/bash

set -e

echo "Starting build process..."

# 创建bin目录（如果不存在）
if [ ! -d "../bin" ]; then
    mkdir -p "../bin"
    echo "Created bin directory"
fi

# 设置编译选项
export LDFLAGS="-s -w"
export CGO_ENABLED=0

# 编译Linux版本
echo
echo "Building Linux binaries..."
export GOOS=linux
export GOARCH=amd64

echo "Building client for Linux..."
go build -trimpath -ldflags "$LDFLAGS" -o "../bin/client" "../cmd/client/main.go"

echo "Building server for Linux..."
go build -trimpath -ldflags "$LDFLAGS" -o "../bin/server" "../cmd/server/main.go"

# 设置可执行权限
chmod +x "../bin/client"
chmod +x "../bin/server"

echo
echo "Build completed successfully!"
echo
echo "Generated files:"
echo "  Linux: client, server"
echo
echo "All binaries are located in: ../bin/"

# 显示文件信息
echo
echo "File details:"
ls -lh ../bin/