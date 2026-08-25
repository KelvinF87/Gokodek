#!/usr/bin/env bash
# Script de instalación multiplataforma (Linux & macOS) para Gokodek
set -e

echo "=== Instalador de Gokodek para Linux / macOS ==="

# Detect OS & Arch
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64) GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *) echo "Arquitectura no soportada: $ARCH"; exit 1 ;;
esac

INSTALL_DIR="$HOME/.local/bin"
mkdir -p "$INSTALL_DIR"

if command -v go >/dev/null 2>&1; then
    echo "Compilando Gokodek desde código fuente..."
    go build -o "$INSTALL_DIR/gokodek" .
else
    echo "Go no encontrado. Asegúrate de tener gokodek compilado en el directorio."
    if [ -f "./gokodek" ]; then
        cp ./gokodek "$INSTALL_DIR/gokodek"
    else
        echo "Error: no se encontró ejecutable gokodek ni compilador Go."
        exit 1
    fi
fi

chmod +x "$INSTALL_DIR/gokodek"

echo ""
echo "¡Gokodek se ha instalado exitosamente en $INSTALL_DIR/gokodek!"
echo ""
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo "Asegúrate de agregar $INSTALL_DIR a tu PATH en ~/.bashrc o ~/.zshrc:"
    echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
