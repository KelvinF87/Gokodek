#!/usr/bin/env bash
# Script de compilación multiplataforma de binarios de Gokodek (Cross-compilation)
set -e

echo "=== Compilando Gokodek para Linux, macOS y Windows ==="

mkdir -p dist

echo "Compilando para Linux (amd64)..."
GOOS=linux GOARCH=amd64 go build -o dist/gokodek-linux-amd64 .

echo "Compilando para Linux (arm64)..."
GOOS=linux GOARCH=arm64 go build -o dist/gokodek-linux-arm64 .

echo "Compilando para macOS (Intel amd64)..."
GOOS=darwin GOARCH=amd64 go build -o dist/gokodek-darwin-amd64 .

echo "Compilando para macOS (Apple Silicon arm64)..."
GOOS=darwin GOARCH=arm64 go build -o dist/gokodek-darwin-arm64 .

echo "Compilando para Windows (amd64)..."
GOOS=windows GOARCH=amd64 go build -o dist/gokodek-windows-amd64.exe .

echo ""
echo "Binarios generados exitosamente en la carpeta dist/:"
ls -la dist/
