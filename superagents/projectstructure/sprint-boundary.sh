#!/usr/bin/env bash
# sprint-boundary.sh — Sprint boundary verification script
# Usage:
#   ./sprint-boundary.sh --end S14    # Run at end of sprint S14
#   ./sprint-boundary.sh --start S15  # Run at start of sprint S15
#
# This script enforces the PRE-SPRINT GATE and sprint-end protocol defined in CLAUDE.md.
#
# Exit codes:
#   0 — All checks passed
#   1 — One or more checks failed
#   2 — Usage error (invalid arguments)
set -euo pipefail

# ── Argument parsing ─────────────────────────────────────────────────────────
if [ "$#" -ne 2 ]; then
  echo "ERROR: Invalid arguments."
  echo "Usage: $0 --end S[N] | --start S[N]"
  echo "Example: $0 --end S14"
  echo "Example: $0 --start S15"
  exit 2
fi

MODE="$1"
SPRINT_ARG="$2"

if [[ ! "$SPRINT_ARG" =~ ^S[0-9]+$ ]]; then
  echo "ERROR: Sprint argument must be in the form S[N] (e.g. S14, S15)."
  exit 2
fi

if [ "$MODE" != "--end" ] && [ "$MODE" != "--start" ]; then
  echo "ERROR: First argument must be --end or --start."
  exit 2
fi

# Extract the numeric sprint number from e.g. "S14" → "14"
SPRINT_NUM="${SPRINT_ARG#S}"
SPRINT_PREV="S$((SPRINT_NUM - 1))"
SPRINT_NEXT="S$((SPRINT_NUM + 1))"

PROJECT_DIR="$(pwd)"
PROJECT_NAME="$(basename "$PROJECT_DIR")"
GATE_FAILURES=0

echo "=== SPRINT BOUNDARY CHECK ==="
echo "Project: ${PROJECT_NAME}"
echo "Mode:    ${MODE}"
echo "Sprint:  ${SPRINT_ARG}"
echo "Date:    $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

# ── Helper: file_age_seconds ─────────────────────────────────────────────────
# Returns the age of a file in seconds. Handles macOS (stat -f %m) and Linux (stat -c %Y).
file_age_seconds() {
  local filepath="$1"
  local mod_epoch
  if stat -f "%m" "$filepath" &>/dev/null 2>&1; then
    # macOS
    mod_epoch=$(stat -f "%m" "$filepath")
  else
    # Linux / GNU coreutils
    mod_epoch=$(stat -c "%Y" "$filepath")
  fi
  echo $(( $(date +%s) - mod_epoch ))
}

# ============================================================================
# SPRINT END CHECKS
# ============================================================================
if [ "$MODE" = "--end" ]; then
  echo "--- Sprint END checks for ${SPRINT_ARG} ---"
  echo ""

  # Check 1: All sprint features have code_evaluator_verdict set (not null)
  echo "Check 1: All sprint features have code_evaluator_verdict set..."
  if command -v jq &>/dev/null && [ -f progress.json ]; then
    MISSING_VERDICT=$(jq --arg sprint "$SPRINT_ARG" \
      '[.features[] | select(.sprint == $sprint) | select(.code_evaluator_verdict == null)] | length' \
      progress.json 2>/dev/null || echo "0")
    TOTAL_IN_SPRINT=$(jq --arg sprint "$SPRINT_ARG" \
      '[.features[] | select(.sprint == $sprint)] | length' \
      progress.json 2>/dev/null || echo "0")
    if [ "$MISSING_VERDICT" -gt 0 ]; then
      echo "FAIL: $MISSING_VERDICT of $TOTAL_IN_SPRINT feature(s) in ${SPRINT_ARG} are missing code_evaluator_verdict."
      echo "  ACTION: Invoke the Code Evaluator agent for all incomplete features in ${SPRINT_ARG}."
      echo "  The Code Evaluator must issue PASS or FAIL for every feature before the sprint closes."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: All $TOTAL_IN_SPRINT feature(s) in ${SPRINT_ARG} have code_evaluator_verdict set."
    fi
  else
    echo "WARN: jq not available or progress.json missing — cannot verify code_evaluator_verdict."
  fi

  # Check 2: HARNESS_QUALITY_REPORT.md modified within last 14 days
  echo "Check 2: HARNESS_QUALITY_REPORT.md modified within last 14 days..."
  if [ ! -f "HARNESS_QUALITY_REPORT.md" ]; then
    echo "FAIL: HARNESS_QUALITY_REPORT.md does not exist."
    echo "  ACTION: Invoke the Harness Evaluator agent to produce HARNESS_QUALITY_REPORT.md."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    AGE_SECS=$(file_age_seconds "HARNESS_QUALITY_REPORT.md")
    AGE_DAYS=$(( AGE_SECS / 86400 ))
    MAX_AGE_SECS=$((14 * 86400))
    if [ "$AGE_SECS" -gt "$MAX_AGE_SECS" ]; then
      echo "FAIL: HARNESS_QUALITY_REPORT.md is ${AGE_DAYS} day(s) old (max: 14 days)."
      echo "  ACTION: Invoke the Harness Evaluator agent to refresh HARNESS_QUALITY_REPORT.md."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: HARNESS_QUALITY_REPORT.md is ${AGE_DAYS} day(s) old (within 14-day limit)."
    fi
  fi

  # Check 3: harness_telemetry.jsonl contains a sprint_end event for this sprint
  echo "Check 3: harness_telemetry.jsonl contains sprint_end event for ${SPRINT_ARG}..."
  if [ ! -f "harness_telemetry.jsonl" ]; then
    echo "FAIL: harness_telemetry.jsonl does not exist."
    echo "  ACTION: Create harness_telemetry.jsonl and log a sprint_end event for ${SPRINT_ARG}."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  elif ! grep -q "\"sprint_end\".*\"${SPRINT_ARG}\"" harness_telemetry.jsonl 2>/dev/null; then
    echo "FAIL: No sprint_end event found for ${SPRINT_ARG} in harness_telemetry.jsonl."
    echo "  ACTION: Append a sprint_end event to harness_telemetry.jsonl before closing ${SPRINT_ARG}."
    echo "  Example entry: {\"event\":\"sprint_end\",\"sprint\":\"${SPRINT_ARG}\",\"timestamp\":\"...\"}"
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    echo "PASS: sprint_end event for ${SPRINT_ARG} found in harness_telemetry.jsonl."
  fi

  # Check 4: sprint_contracts/S[N+1].md exists and contains "Documentation Deliverables"
  echo "Check 4: Sprint contract for next sprint (${SPRINT_NEXT}) exists with 'Documentation Deliverables'..."
  if [ ! -f "sprint_contracts/${SPRINT_NEXT}.md" ]; then
    echo "FAIL: sprint_contracts/${SPRINT_NEXT}.md does not exist."
    echo "  ACTION: Invoke the Harness Architect agent to create the sprint contract for ${SPRINT_NEXT}."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  elif ! grep -q "Documentation Deliverables" "sprint_contracts/${SPRINT_NEXT}.md"; then
    echo "FAIL: sprint_contracts/${SPRINT_NEXT}.md exists but is missing the 'Documentation Deliverables' section."
    echo "  ACTION: Invoke the Harness Architect agent to add a 'Documentation Deliverables' section to sprint_contracts/${SPRINT_NEXT}.md."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    echo "PASS: sprint_contracts/${SPRINT_NEXT}.md exists and contains 'Documentation Deliverables'."
  fi

  # Check 5: session_state.json exists and modified within last 24 hours
  echo "Check 5: session_state.json exists and was modified within the last 24 hours..."
  if [ ! -f "session_state.json" ]; then
    echo "FAIL: session_state.json does not exist."
    echo "  ACTION: The implementing agent must write session_state.json during session end protocol."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    AGE_SECS=$(file_age_seconds "session_state.json")
    MAX_AGE_SECS=$((24 * 3600))
    AGE_HOURS=$(( AGE_SECS / 3600 ))
    if [ "$AGE_SECS" -gt "$MAX_AGE_SECS" ]; then
      echo "FAIL: session_state.json is ${AGE_HOURS} hour(s) old (max: 24 hours)."
      echo "  ACTION: The implementing agent must update session_state.json as part of session end protocol."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: session_state.json is ${AGE_HOURS} hour(s) old (within 24-hour limit)."
    fi
  fi

  # Check 6: session_handoff.md modified within last 24 hours
  echo "Check 6: session_handoff.md was modified within the last 24 hours..."
  if [ ! -f "session_handoff.md" ]; then
    echo "FAIL: session_handoff.md does not exist."
    echo "  ACTION: The implementing agent must write session_handoff.md during session end protocol."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    AGE_SECS=$(file_age_seconds "session_handoff.md")
    MAX_AGE_SECS=$((24 * 3600))
    AGE_HOURS=$(( AGE_SECS / 3600 ))
    if [ "$AGE_SECS" -gt "$MAX_AGE_SECS" ]; then
      echo "FAIL: session_handoff.md is ${AGE_HOURS} hour(s) old (max: 24 hours)."
      echo "  ACTION: The implementing agent must update session_handoff.md as part of session end protocol."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: session_handoff.md is ${AGE_HOURS} hour(s) old (within 24-hour limit)."
    fi
  fi

fi

# ============================================================================
# SPRINT START CHECKS
# ============================================================================
if [ "$MODE" = "--start" ]; then
  echo "--- Sprint START checks for ${SPRINT_ARG} ---"
  echo ""

  # Check 1: sprint_contracts/S[N].md exists and is > 100 bytes
  echo "Check 1: sprint_contracts/${SPRINT_ARG}.md exists and is > 100 bytes..."
  if [ ! -f "sprint_contracts/${SPRINT_ARG}.md" ]; then
    echo "FAIL: sprint_contracts/${SPRINT_ARG}.md does not exist."
    echo "  ACTION: Invoke the Harness Architect agent to create the sprint contract for ${SPRINT_ARG}."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    FILE_SIZE=$(wc -c < "sprint_contracts/${SPRINT_ARG}.md")
    if [ "$FILE_SIZE" -le 100 ]; then
      echo "FAIL: sprint_contracts/${SPRINT_ARG}.md is only ${FILE_SIZE} bytes (must be > 100 bytes)."
      echo "  ACTION: Invoke the Harness Architect agent to fill out the sprint contract for ${SPRINT_ARG}."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: sprint_contracts/${SPRINT_ARG}.md exists (${FILE_SIZE} bytes)."
    fi
  fi

  # Check 2: sprint_contracts/S[N].md contains "Documentation Deliverables" section
  echo "Check 2: sprint_contracts/${SPRINT_ARG}.md contains 'Documentation Deliverables' section..."
  if [ -f "sprint_contracts/${SPRINT_ARG}.md" ]; then
    if ! grep -q "Documentation Deliverables" "sprint_contracts/${SPRINT_ARG}.md"; then
      echo "FAIL: sprint_contracts/${SPRINT_ARG}.md is missing the 'Documentation Deliverables' section."
      echo "  ACTION: Invoke the Harness Architect agent to add a 'Documentation Deliverables' section."
      echo "  The Harness Architect must ensure every sprint contract includes documentation scope."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: sprint_contracts/${SPRINT_ARG}.md contains 'Documentation Deliverables'."
    fi
  else
    echo "SKIP: sprint_contracts/${SPRINT_ARG}.md does not exist — check 1 already failed."
  fi

  # Check 3: HARNESS_QUALITY_REPORT.md contains "PASS" or "CONDITIONAL PASS"
  echo "Check 3: HARNESS_QUALITY_REPORT.md contains a PASS or CONDITIONAL PASS verdict..."
  if [ ! -f "HARNESS_QUALITY_REPORT.md" ]; then
    echo "FAIL: HARNESS_QUALITY_REPORT.md does not exist."
    echo "  ACTION: Invoke the Harness Evaluator agent to produce HARNESS_QUALITY_REPORT.md with a PASS verdict."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  elif ! grep -qE "CONDITIONAL PASS|Overall Verdict.*PASS|^PASS" "HARNESS_QUALITY_REPORT.md"; then
    echo "FAIL: HARNESS_QUALITY_REPORT.md does not contain a PASS or CONDITIONAL PASS verdict."
    echo "  ACTION: Invoke the Harness Evaluator agent to audit and issue a PASS verdict before work begins."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    echo "PASS: HARNESS_QUALITY_REPORT.md contains an acceptable verdict."
  fi

  # Check 4: progress.json contains .features array with at least one entry
  echo "Check 4: progress.json contains at least one feature..."
  if [ ! -f "progress.json" ]; then
    echo "FAIL: progress.json does not exist."
    echo "  ACTION: The Harness Architect must initialise progress.json with feature-level tracking."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  elif command -v jq &>/dev/null; then
    FEATURE_COUNT=$(jq '.features | length' progress.json 2>/dev/null || echo "0")
    if [ "$FEATURE_COUNT" -eq 0 ]; then
      echo "FAIL: progress.json has no features (empty .features array)."
      echo "  ACTION: The Harness Architect must populate progress.json with sprint features."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: progress.json has ${FEATURE_COUNT} feature(s)."
    fi
  else
    echo "WARN: jq not available — cannot verify progress.json feature count. Install jq for full validation."
  fi

  # Check 5: All features from prior sprint (S[N-1]) have code_evaluator_verdict set
  echo "Check 5: All features from prior sprint (${SPRINT_PREV}) have code_evaluator_verdict set..."
  if command -v jq &>/dev/null && [ -f progress.json ]; then
    PREV_MISSING=$(jq --arg sprint "$SPRINT_PREV" \
      '[.features[] | select(.sprint == $sprint) | select(.code_evaluator_verdict == null)] | length' \
      progress.json 2>/dev/null || echo "0")
    PREV_TOTAL=$(jq --arg sprint "$SPRINT_PREV" \
      '[.features[] | select(.sprint == $sprint)] | length' \
      progress.json 2>/dev/null || echo "0")
    if [ "$PREV_TOTAL" -eq 0 ]; then
      echo "SKIP: No features found for ${SPRINT_PREV} in progress.json — check skipped."
    elif [ "$PREV_MISSING" -gt 0 ]; then
      echo "FAIL: $PREV_MISSING of $PREV_TOTAL feature(s) from ${SPRINT_PREV} are missing code_evaluator_verdict."
      echo "  ACTION: Invoke the Code Evaluator agent for all unevaluated features from ${SPRINT_PREV}."
      echo "  The Code Evaluator must close out the prior sprint before ${SPRINT_ARG} begins."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: All $PREV_TOTAL feature(s) from ${SPRINT_PREV} have code_evaluator_verdict set."
    fi
  else
    echo "WARN: jq not available or progress.json missing — cannot verify prior sprint evaluation."
  fi

  # Check 6: go test -race ./... exits 0
  echo "Check 6: go test -race ./... exits 0..."
  if command -v go &>/dev/null && [ -f "go.mod" ]; then
    if ! go test -race ./... > /dev/null 2>&1; then
      echo "FAIL: go test -race ./... is not green."
      echo "  ACTION: Fix all failing tests before beginning ${SPRINT_ARG} implementation."
      echo "  No new feature work may begin on a red test suite."
      GATE_FAILURES=$((GATE_FAILURES + 1))
    else
      echo "PASS: go test -race ./... is green."
    fi
  else
    echo "WARN: go not available or go.mod missing — cannot run test suite. Verify tests pass manually."
  fi

  # Check 7: session_state.json and session_handoff.md both exist
  echo "Check 7: session_state.json and session_handoff.md both exist..."
  MISSING_FILES=0
  if [ ! -f "session_state.json" ]; then
    echo "FAIL: session_state.json does not exist."
    echo "  ACTION: The implementing agent must write session_state.json during session end protocol."
    MISSING_FILES=$((MISSING_FILES + 1))
    GATE_FAILURES=$((GATE_FAILURES + 1))
  fi
  if [ ! -f "session_handoff.md" ]; then
    echo "FAIL: session_handoff.md does not exist."
    echo "  ACTION: The implementing agent must write session_handoff.md during session end protocol."
    MISSING_FILES=$((MISSING_FILES + 1))
    GATE_FAILURES=$((GATE_FAILURES + 1))
  fi
  if [ "$MISSING_FILES" -eq 0 ]; then
    echo "PASS: session_state.json and session_handoff.md both exist."
  fi

  # Check 8: harness_telemetry.jsonl contains a sprint_start event for S[N]
  echo "Check 8: harness_telemetry.jsonl contains sprint_start event for ${SPRINT_ARG}..."
  if [ ! -f "harness_telemetry.jsonl" ]; then
    echo "FAIL: harness_telemetry.jsonl does not exist."
    echo "  ACTION: Create harness_telemetry.jsonl and log a sprint_start event for ${SPRINT_ARG}."
    GATE_FAILURES=$((GATE_FAILURES + 1))
  elif ! grep -q "\"sprint_start\".*\"${SPRINT_ARG}\"" harness_telemetry.jsonl 2>/dev/null; then
    echo "FAIL: No sprint_start event found for ${SPRINT_ARG} in harness_telemetry.jsonl."
    echo "  ACTION: Append a sprint_start event to harness_telemetry.jsonl."
    echo "  Example: {\"event\":\"sprint_start\",\"sprint\":\"${SPRINT_ARG}\",\"timestamp\":\"...\"}"
    GATE_FAILURES=$((GATE_FAILURES + 1))
  else
    echo "PASS: sprint_start event for ${SPRINT_ARG} found in harness_telemetry.jsonl."
  fi

fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "=== SUMMARY ==="
echo "Mode:     ${MODE} ${SPRINT_ARG}"
echo "Failures: ${GATE_FAILURES}"
echo ""

if [ "$GATE_FAILURES" -gt 0 ]; then
  echo "=== SPRINT BOUNDARY CHECKS FAILED ==="
  echo "Fix ${GATE_FAILURES} failure(s) before proceeding."
  echo "Escalate to the Engineering Orchestrator if you cannot resolve the failures."
  exit 1
fi

echo "=== ALL SPRINT BOUNDARY CHECKS PASSED ==="
exit 0
