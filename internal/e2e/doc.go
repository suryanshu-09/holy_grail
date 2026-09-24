// Package e2e is the automated end-to-end harness for Phase 22 (#27).
//
// It scripts the full user-visible flow as a Go test driver:
//
//	Upload PDF → Process PDF → Extract questions → Classify topics →
//	Generate embeddings → Select topic → Retrieve questions →
//	Generate quiz → Answer quiz → See results
//
// The driver (Flow) is transport-agnostic: each stage is a small interface,
// so unit-speed runs use the scripted fakes in fakes.go (no real API keys,
// LLM, or Postgres required). The live variant (live_test.go) replays the
// same stage order against a real server + database and self-skips unless
// E2E_LIVE=1 with the required environment (see RUN_LIVE.md).
package e2e
