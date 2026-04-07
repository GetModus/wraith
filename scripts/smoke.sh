#!/usr/bin/env bash
# smoke.sh — Replay fixtures through WRAITH queue and verify dedup/state outcomes.
# Runs offline (no server needed). Uses Go test binary directly.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
FIXTURES="$REPO_DIR/fixtures"

echo "==> WRAITH Smoke Test"
echo "    Fixtures: $FIXTURES"
echo ""

# Phase 1: Verify fixtures exist
echo "--- Phase 1: Fixture inventory ---"
CAPTURE_COUNT=$(find "$FIXTURES/captures" -name "*.json" 2>/dev/null | wc -l | tr -d ' ')
EXPECTED_COUNT=$(find "$FIXTURES/expected" -name "*.json" -o -name "*.jsonl" 2>/dev/null | wc -l | tr -d ' ')
echo "    Capture fixtures: $CAPTURE_COUNT"
echo "    Expected fixtures: $EXPECTED_COUNT"

if [ "$CAPTURE_COUNT" -lt 1 ]; then
    echo "FAIL: No capture fixtures found in $FIXTURES/captures/"
    exit 1
fi

# Phase 2: Validate fixture JSON
echo ""
echo "--- Phase 2: Fixture JSON validation ---"
INVALID=0
for f in "$FIXTURES/captures"/*.json "$FIXTURES/expected"/*.json; do
    [ -f "$f" ] || continue
    if ! python3 -m json.tool "$f" > /dev/null 2>&1; then
        echo "    INVALID JSON: $f"
        INVALID=$((INVALID + 1))
    fi
done
if [ "$INVALID" -gt 0 ]; then
    echo "FAIL: $INVALID fixture files have invalid JSON"
    exit 1
fi
echo "    All fixture JSON valid."

# Phase 3: Run Go unit tests
echo ""
echo "--- Phase 3: Go unit tests ---"
cd "$REPO_DIR"
if go test ./internal/... -count=1 2>&1; then
    echo "    All WRAITH tests pass."
else
    echo "FAIL: WRAITH Go tests failed"
    exit 1
fi

# Phase 4: Fixture replay — run the fixture replay test
echo ""
echo "--- Phase 4: Fixture replay (dedup verification) ---"
cd "$REPO_DIR"
if go test ./internal/wraith/ -run TestFixtureReplayDedup -v -count=1 2>&1; then
    echo "    Fixture replay: PASS"
else
    echo "FAIL: Fixture replay test failed"
    exit 1
fi

echo ""
echo "==> WRAITH Smoke Test: ALL PHASES PASS"
