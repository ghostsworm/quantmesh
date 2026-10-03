#!/bin/bash
# Canonical frontend-first verified single-binary build. No deployment.
set -euo pipefail
task_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$task_root"
exec make build
