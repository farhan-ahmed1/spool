#!/bin/bash

# Spool - Distributed Task Queue Setup Script
# This script sets up your development environment

set -e

echo " Setting up Spool development environment..."

# Check prerequisites
command -v go >/dev/null 2>&1 || { echo " Go is required but not installed. Aborting." >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo " Docker is required but not installed. Aborting." >&2; exit 1; }

echo " Prerequisites check passed"

# Install dependencies
echo "📚 Installing Go dependencies..."
go mod download
go mod tidy

# Install development tools
echo "🔧 Installing development tools..."
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
mkdir -p tests/load

# Scripts
mkdir -p scripts

# Deployments
mkdir -p deployments/kubernetes

# Install dependencies
echo "📚 Installing dependencies..."
go get github.com/redis/go-redis/v9
go get github.com/google/uuid
go get github.com/gorilla/mux
go get github.com/gorilla/websocket
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
go get github.com/stretchr/testify/assert
go get go.uber.org/zap

echo " Project structure created!"
echo ""
echo "Next steps:"
echo " 1. cd $PROJECT_NAME"
echo " 2. Update go.mod with your GitHub username"
echo " 3. Start coding!"
echo ""
echo "Quick start:"
echo " make setup  - Install dev dependencies"
echo " make dev   - Run in dev mode"
echo " make test   - Run tests"
