#!/usr/bin/env bash
# Build MdReader.exe (Windows). Works from Linux/macOS (cross-compilation) and
# from Windows (Git Bash / WSL). Pure Go: no cgo, no external dependencies.
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p dist
export CGO_ENABLED=0 GOOS=windows GOARCH=amd64
go build -trimpath -ldflags="-H windowsgui -s -w" -o dist/MdReader.exe .

echo "OK -> dist/MdReader.exe"
ls -l dist/MdReader.exe
