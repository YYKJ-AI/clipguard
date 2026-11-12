#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
if [ "${1:-}" = "--online" ]; then
    : "${LLM_BASE_URL:?Set LLM_BASE_URL to your own Chat Completions base URL}"
    : "${LLM_MODEL:?Set LLM_MODEL to your model name}"
else
    unset LLM_BASE_URL LLM_MODEL LLM_API_KEY LLM_JSON_MODE LLM_THINKING LLM_MAX_TOKENS
fi
unset PRODUCT_AUTH_FILE
export LISTEN_ADDR="127.0.0.1:${PORT:-8090}"
mkdir -p bin
go build -trimpath -o bin/safety-edit-agent .
printf 'Open http://%s in your browser. Press Ctrl+C to stop.\n' "$LISTEN_ADDR"
exec ./bin/safety-edit-agent
