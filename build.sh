#!/bin/sh
# Construit les trois exécutables Windows (aucun compilateur C nécessaire).
set -e
export CGO_ENABLED=0
LDFLAGS_GUI="-H windowsgui -s -w"
LDFLAGS_CON="-s -w"
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$LDFLAGS_GUI" -o dist/MdReader.exe .
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$LDFLAGS_CON" -o dist/MdReader-debug.exe .
GOOS=windows GOARCH=386 go build -trimpath -ldflags="$LDFLAGS_GUI" -o dist/MdReader-32bit.exe .
ls -la dist/*.exe
