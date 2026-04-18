## Sprint / Feature

Sprint: <!-- e.g. S3 -->
Feature IDs: <!-- e.g. F-008, F-009 -->

## Evaluator Sign-off

- [ ] Code Evaluator PASS recorded in `HARNESS_QUALITY_REPORT.md`
- [ ] Security Evaluator PASS (if auth, crypto, or external API work)
- [ ] Architecture Evaluator PASS (if every 4th sprint or production deploy)

## Session Artefacts Updated

- [ ] `progress.json` updated with feature-level verdicts (not sprint-level)
- [ ] `session_state.json` updated
- [ ] `harness_telemetry.jsonl` has `session_end` event
- [ ] `session_handoff.md` written
- [ ] Sprint contract `sprint_contracts/[SN].md` references this PR

## Quality Gates

- [ ] `go test -race ./...` passes
- [ ] `golangci-lint run` exits 0
- [ ] Coverage >= 85%
- [ ] `gosec ./...` passes with zero high-severity findings
- [ ] `govulncheck ./...` exits 0

## Documentation Deliverables

- [ ] Package-level doc comments for all exported types
- [ ] `shared_knowledge.md` appended with any non-obvious discoveries
- [ ] `knowledge_base/` updated if new domain knowledge found
- [ ] `decision_log.md` updated if any ADR-worthy decisions made
- [ ] Example code updated if public API changed

## Summary

<!-- What was built, why, any deviations from sprint contract -->
