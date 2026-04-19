#!/usr/bin/env bash
# Hyperspace — Session Initialisation Gate
# Run this at the start of every agent session. Fails fast if gates are not met.
#
# Usage: ./init.sh [SPRINT_ID]
# Example: ./init.sh S3
#
# Exit codes:
#   0 — All gates passed; session may begin
#   1 — A mandatory gate failed; session must not begin
#   2 — Usage error (invalid arguments)
set -euo pipefail

SPRINT="${1:-}"
PROJECT_DIR="$(pwd)"
PROJECT_NAME="$(basename "$PROJECT_DIR")"
FAILURES=0
WARNINGS=0

echo "=== SESSION INIT GATE ==="
echo "Project: ${PROJECT_NAME}"
echo "Sprint:  ${SPRINT:-<not specified>}"
echo "Date:    $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

# ── Gate 1: Sprint contract ──────────────────────────────────────────────────
if [ -n "$SPRINT" ]; then
  if [ ! -f "sprint_contracts/${SPRINT}.md" ]; then
    echo "GATE 1 FAIL: sprint_contracts/${SPRINT}.md does not exist."
    echo "  ACTION: The Harness Architect must create the sprint contract before work begins."
    echo "  HARD STOP #1: No sprint contract exists."
    FAILURES=$((FAILURES + 1))
  elif [ ! -s "sprint_contracts/${SPRINT}.md" ]; then
    echo "GATE 1 FAIL: sprint_contracts/${SPRINT}.md exists but is empty."
    echo "  ACTION: The sprint contract must have content before work begins."
    FAILURES=$((FAILURES + 1))
  else
    echo "GATE 1 PASS: Sprint contract sprint_contracts/${SPRINT}.md found and non-empty"
  fi
else
  echo "GATE 1 SKIP: No sprint specified — sprint contract check skipped"
fi

# ── Gate 2: Harness Quality Report ──────────────────────────────────────────
if [ ! -f "HARNESS_QUALITY_REPORT.md" ]; then
  echo "GATE 2 FAIL: HARNESS_QUALITY_REPORT.md does not exist."
  echo "  ACTION: The Harness Evaluator must produce a PASS verdict before implementation begins."
  echo "  HARD STOP #2: Harness Evaluator has not issued PASS."
  FAILURES=$((FAILURES + 1))
elif grep -q "FAIL" "HARNESS_QUALITY_REPORT.md" && ! grep -q "CONDITIONAL PASS\|Overall Verdict.*PASS" "HARNESS_QUALITY_REPORT.md"; then
  echo "GATE 2 FAIL: HARNESS_QUALITY_REPORT.md contains a FAIL verdict."
  echo "  ACTION: The Harness Evaluator must issue PASS or CONDITIONAL PASS before implementation begins."
  FAILURES=$((FAILURES + 1))
else
  echo "GATE 2 PASS: HARNESS_QUALITY_REPORT.md exists with acceptable verdict"
fi

# ── Gate 3: Baseline tests ──────────────────────────────────────────────────
echo "Running baseline tests..."
if command -v go &> /dev/null && [ -f "go.mod" ]; then
  if ! go test -race ./... > /dev/null 2>&1; then
    echo "GATE 3 FAIL: go test -race ./... is not green."
    echo "  ACTION: Fix failing tests before starting new work."
    echo "  HARD STOP #3: Baseline tests are not green."
    FAILURES=$((FAILURES + 1))
  else
    echo "GATE 3 PASS: Baseline tests green (go test -race ./...)"
  fi
elif [ -f "Makefile" ] && grep -q "^test:" "Makefile"; then
  if ! make test > /dev/null 2>&1; then
    echo "GATE 3 FAIL: make test is not green."
    echo "  ACTION: Fix failing tests before starting new work."
    echo "  HARD STOP #3: Baseline tests are not green."
    FAILURES=$((FAILURES + 1))
  else
    echo "GATE 3 PASS: Baseline tests green (make test)"
  fi
else
  echo "GATE 3 WARN: No test runner detected. Cannot verify baseline."
  WARNINGS=$((WARNINGS + 1))
fi

# ── Gate 4: session_state.json ───────────────────────────────────────────────
if [ ! -f "session_state.json" ]; then
  echo "GATE 4 WARN: session_state.json does not exist. Creating bootstrap state."
  cat > session_state.json <<EOF
{
  "project": "${PROJECT_NAME}",
  "last_updated": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "state": "ready",
  "current_sprint": "${SPRINT:-unknown}",
  "sprints_complete": []
}
EOF
  WARNINGS=$((WARNINGS + 1))
else
  echo "GATE 4 PASS: session_state.json exists"
fi

# ── Gate 5: progress.json is feature-level ───────────────────────────────────
if [ ! -f "progress.json" ]; then
  echo "GATE 5 FAIL: progress.json does not exist."
  echo "  ACTION: The Harness Architect must initialise progress.json with feature-level tracking."
  FAILURES=$((FAILURES + 1))
elif command -v jq &> /dev/null; then
  FEATURE_COUNT=$(jq '.features | length' progress.json 2>/dev/null || echo "0")
  if [ "$FEATURE_COUNT" -eq 0 ]; then
    echo "GATE 5 FAIL: progress.json has no features. It may be using sprint-level tracking."
    echo "  ACTION: HARD STOP #6: progress.json must track at feature level."
    FAILURES=$((FAILURES + 1))
  else
    echo "GATE 5 PASS: progress.json has ${FEATURE_COUNT} features (feature-level tracking confirmed)"
  fi
else
  echo "GATE 5 WARN: jq not installed — cannot validate progress.json schema. Install jq for full validation."
  WARNINGS=$((WARNINGS + 1))
fi

# ── Gate 6: harness_telemetry.jsonl exists ───────────────────────────────────
if [ ! -f "harness_telemetry.jsonl" ]; then
  echo "GATE 6 WARN: harness_telemetry.jsonl does not exist. Creating empty file."
  touch harness_telemetry.jsonl
  WARNINGS=$((WARNINGS + 1))
else
  echo "GATE 6 PASS: harness_telemetry.jsonl exists"
fi

# ── Gate 7: Sprint start event logged ────────────────────────────────────────
if [ -n "$SPRINT" ] && [ -f "harness_telemetry.jsonl" ]; then
  if ! grep -q "\"sprint_start\".*\"${SPRINT}\"" harness_telemetry.jsonl 2>/dev/null; then
    echo "GATE 7 WARN: No sprint_start event found for ${SPRINT} in harness_telemetry.jsonl."
    echo "  Appending sprint_start event now."
    echo "{\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"project\":\"${PROJECT_NAME}\",\"event\":\"sprint_start\",\"agent\":\"init.sh\",\"detail\":\"Sprint ${SPRINT} session initialised\",\"sprint\":\"${SPRINT}\"}" >> harness_telemetry.jsonl
    WARNINGS=$((WARNINGS + 1))
  else
    echo "GATE 7 PASS: sprint_start event for ${SPRINT} found in telemetry"
  fi
fi

# ── Gate 8: Code Evaluator verdict present for prior sprint features ──────────
# At least one feature in progress.json has code_evaluator_verdict=PASS.
# Catches the case where a new sprint begins before evaluation of the prior sprint is complete.
if command -v jq &>/dev/null && [ -f progress.json ]; then
  EVAL_PASS=$(jq '[.features[] | select(.code_evaluator_verdict == "PASS")] | length' progress.json)
  if [ "$EVAL_PASS" -eq 0 ]; then
    echo "GATE 8 FAIL: No feature in progress.json has code_evaluator_verdict=PASS."
    echo "  ACTION: Invoke the Code Evaluator agent before beginning implementation."
    echo "  The Code Evaluator must issue PASS for at least one feature before new work begins."
    FAILURES=$((FAILURES + 1))
  else
    echo "GATE 8 PASS: $EVAL_PASS feature(s) have Code Evaluator PASS verdict."
  fi
fi

# ── Gate 9: Harness Evaluator event present for current sprint ───────────────
# harness_telemetry.jsonl must contain a harness-evaluator event for the current sprint.
if [ -f harness_telemetry.jsonl ]; then
  if ! grep -q "harness.evaluator\|harness_evaluator\|harness-evaluator" harness_telemetry.jsonl; then
    echo "GATE 9 WARN: No Harness Evaluator event found in harness_telemetry.jsonl."
    echo "  ACTION: Invoke the Harness Evaluator agent to audit the prior sprint"
    echo "  before beginning implementation. Update HARNESS_QUALITY_REPORT.md."
    # Warn only — not a hard stop, since evaluator may be pending
  else
    echo "GATE 9 PASS: Harness Evaluator event found in telemetry."
  fi
fi

# ── Gate 10: Security evaluator required for identity/secrets packages ────────
# If current sprint targets security-sensitive packages, check security_evaluator_verdict.
if command -v jq &>/dev/null && [ -f progress.json ]; then
  SEC_REQUIRED_MISSING=$(jq '[.features[] | select(.security_required == true) | select(.status == "code_complete" or .status == "evaluator_pass") | select(.security_evaluator_verdict == null)] | length' progress.json)
  if [ "$SEC_REQUIRED_MISSING" -gt 0 ]; then
    echo "GATE 10 FAIL: $SEC_REQUIRED_MISSING security-required feature(s) lack a Security Evaluator verdict."
    echo "  ACTION: Invoke the Security Evaluator agent for features with security_required=true"
    echo "  before they can be marked evaluator_pass."
    FAILURES=$((FAILURES + 1))
  else
    echo "GATE 10 PASS: All security-required features have Security Evaluator verdicts."
  fi
fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "=== SUMMARY ==="
echo "Failures: ${FAILURES}"
echo "Warnings: ${WARNINGS}"
echo ""

if [ "$FAILURES" -gt 0 ]; then
  echo "=== GATES FAILED — session MUST NOT begin ==="
  echo "Fix ${FAILURES} failure(s) before starting any implementation work."
  echo "Escalate to the Engineering Orchestrator if you cannot resolve the failures."
  exit 1
fi

if [ "$WARNINGS" -gt 0 ]; then
  echo "=== ALL GATES PASSED (with ${WARNINGS} warning(s)) — session may begin ==="
else
  echo "=== ALL GATES PASSED — session may begin ==="
fi
exit 0
