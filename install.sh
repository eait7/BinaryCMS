#!/bin/bash
set -e

echo "============================================="
echo "        Welcome to BinaryCMS Installer       "
echo "============================================="

# Ensure Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Error: Go is not installed. Please install Go (https://go.dev/doc/install) and try again."
    exit 1
fi

# Clone repository
if [ -d "BinaryCMS" ]; then
    echo "⚠️  Directory 'BinaryCMS' already exists. Updating..."
    cd BinaryCMS
    git pull origin main
else
    echo "📥 Cloning BinaryCMS repository..."
    git clone https://github.com/eait7/BinaryCMS.git
    cd BinaryCMS
fi

echo "🏗  Building core CMS server..."
go build -o gocms_server ./cmd/server

echo "🔌 Creating empty plugins directory..."
mkdir -p plugins

echo "✅ Build complete!"
echo ""
echo "============================================="
echo "   BinaryCMS has been installed successsfully!   "
echo "============================================="
echo ""
echo "To start the server, run:"
echo "  cd BinaryCMS && ./gocms_server"
echo ""
echo "The CMS will be available at http://localhost:8080"
echo "Admin panel: http://localhost:8080/admin (Default: admin / admin)"
