#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "Building tunnerse API for Linux and Windows..."
echo ""

mkdir -p bin

# Build for Linux
echo "Building for Linux..."
echo "  - API..."
GOOS=linux GOARCH=amd64 go build -o bin/tunnerse-api ./cmd/api
echo "  ✓ API built: bin/tunnerse-api"

echo ""

# Build for Windows
echo "Building for Windows..."
echo "  - API..."
GOOS=windows GOARCH=amd64 go build -o bin/tunnerse-api.exe ./cmd/api
echo "  ✓ API built: bin/tunnerse-api.exe"

echo ""
echo "Build complete! Binaries are in the bin/ directory:"
echo ""
echo "Linux:"
echo "  - ./bin/tunnerse-api"
echo ""
echo "Windows:"
echo "  - bin/tunnerse-api.exe"
echo ""
