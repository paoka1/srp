@echo off
setlocal enabledelayedexpansion

echo Starting build process...

:: 创建bin目录（如果不存在）
if not exist "..\bin" (
    mkdir "..\bin"
    echo Created bin directory
)

:: 设置编译选项
set LDFLAGS=-s -w
set CGO_ENABLED=0

:: 编译Windows版本
echo.
echo Building Windows binaries...
set GOOS=windows
set GOARCH=amd64

echo Building client for Windows...
go build -trimpath -ldflags "%LDFLAGS%" -o "..\bin\client.exe" "..\cmd\client\main.go"
if !errorlevel! neq 0 (
    echo Failed to build client for Windows
    exit /b 1
)

echo Building server for Windows...
go build -trimpath -ldflags "%LDFLAGS%" -o "..\bin\server.exe" "..\cmd\server\main.go"
if !errorlevel! neq 0 (
    echo Failed to build server for Windows
    exit /b 1
)

:: 编译Linux版本
echo.
echo Building Linux binaries...
set GOOS=linux
set GOARCH=amd64

echo Building client for Linux...
go build -trimpath -ldflags "%LDFLAGS%" -o "..\bin\client_linux" "..\cmd\client\main.go"
if !errorlevel! neq 0 (
    echo Failed to build client for Linux
    exit /b 1
)

echo Building server for Linux...
go build -trimpath -ldflags "%LDFLAGS%" -o "..\bin\server_linux" "..\cmd\server\main.go"
if !errorlevel! neq 0 (
    echo Failed to build server for Linux
    exit /b 1
)

echo.
echo Build completed successfully!
echo.
echo Generated files:
echo   Windows: client.exe, server.exe
echo   Linux:   client_linux, server_linux
echo.
echo All binaries are located in: ..\bin\

endlocal