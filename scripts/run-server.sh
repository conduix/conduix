#!/bin/bash
# Run the control-plane server with .env configuration

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Load .env file if it exists
ENV_FILE="$PROJECT_ROOT/.env"
if [ -f "$ENV_FILE" ]; then
    echo "Loading environment from $ENV_FILE"
    set -a
    source "$ENV_FILE"
    set +a
else
    echo "Warning: .env file not found at $ENV_FILE"
    echo "Copy .env.example to .env and configure your settings"
    exit 1
fi

# Change to control-plane directory
cd "$PROJECT_ROOT/control-plane"

# Run the server with migration
echo "Starting Conduix Control Plane..."
go run ./cmd/server/main.go -migrate "$@"
