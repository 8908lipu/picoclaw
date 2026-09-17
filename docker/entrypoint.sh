#!/bin/sh
set -e

PICO_DIR="${HOME:-/root}/.picoclaw"
mkdir -p "${PICO_DIR}/workspace/skills"
mkdir -p "${PICO_DIR}/skills"

# Populate skills from builtin image directory if present
if [ -d "/skills" ]; then
    cp -rn /skills/* "${PICO_DIR}/workspace/skills/" 2>/dev/null || true
    cp -rn /skills/* "${PICO_DIR}/skills/" 2>/dev/null || true
fi

export PICOCLAW_BUILTIN_SKILLS="/skills"

# Remove stale PID file from any previous container run.
rm -f "${PICO_DIR}/.picoclaw.pid"

# Bind to Render PORT if provided
if [ -n "$PORT" ]; then
    export PICOCLAW_GATEWAY_PORT="$PORT"
    export PICOCLAW_GATEWAY_HOST="${PICOCLAW_GATEWAY_HOST:-0.0.0.0}"
fi

exec picoclaw gateway "$@"
