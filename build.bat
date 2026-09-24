@echo off
rem Build MdReader.exe on Windows. Requires Go 1.21+ (no C compiler needed).
setlocal
cd /d "%~dp0"

if not exist dist mkdir dist
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

go build -trimpath -ldflags="-H windowsgui -s -w" -o dist\MdReader.exe .
if errorlevel 1 (
  echo.
  echo Build failed.
  exit /b 1
)

echo.
echo OK -^> dist\MdReader.exe
dir /b /-c dist\MdReader.exe
endlocal
