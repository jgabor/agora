# Plan: Deepen the Cast Module

Consolidate agent identity and "theatre" logic into a dedicated `internal/cast` module to improve locality and leverage.

## Objective
Currently, agent identity logic (names like "Solon", colors, ordinals) is scattered across `internal/types`, `internal/output`, and `internal/orchestrator`. This makes it hard to maintain the "theatre" of Agora and leads to duplication (e.g., `agentAccent` in `render.go` vs. `CastMember.Color`).

## Proposed Changes

### 1. Create `internal/cast` package
- Define a `Cast` module that manages the mapping between `agent_id` and `CastMember`.
- Move `castNames` and `castColors` pools here.
- Implement logic to assign identities to agents, ensuring stability even when resuming or extending transcripts.

### 2. Refactor `internal/types`
- Keep `CastMember` and `TranscriptMetadata` as DTOs for serialization.
- Remove `BuildCast`, `CastMemberForAgent`, and the name/color pools.
- Update `NewTranscriptMetadata` to use the new `cast` module (or let the orchestrator handle it).

### 3. Refactor `internal/output`
- Update `OutputManager` to use the `cast.Cast` module instead of internal maps.
- Remove `agentAccent` from `render.go` and move its fallback logic into the `cast` module.
- Consolidate badge rendering (`[A1 strategist]`) into the `cast` module.

### 4. Refactor `internal/orchestrator`
- Ensure the orchestrator correctly initializes the cast when starting or resuming a deliberation.

## Implementation Steps

### Phase 1: Research & Scaffolding
- [ ] Create `internal/cast/cast.go` with the core `Cast` struct and its interface.
- [ ] Implement `New(agents []types.AgentConfig) *Cast`.
- [ ] Implement `FromMetadata(metadata *types.TranscriptMetadata) *Cast`.

### Phase 2: Refactoring Types
- [ ] Move `castNames` and `castColors` to `internal/cast/cast.go`.
- [ ] Update `internal/types/types.go` to remove redundant logic.

### Phase 3: Refactoring Output
- [ ] Update `internal/output/output.go` to use `cast.Cast`.
- [ ] Move `agentAccent` logic to `internal/cast`.
- [ ] Update `internal/output/render.go` to use the new cast-based identity.

### Phase 4: Integration & Cleanup
- [ ] Update `internal/orchestrator/orchestrator.go` if needed.
- [ ] Run all tests and ensure visual consistency in CLI output.

## Verification & Testing
- [ ] New unit tests in `internal/cast/cast_test.go`:
    - Test that same agents get same names/colors.
    - Test that metadata reconstruction works.
    - Test fallback for unknown agents.
- [ ] Run `agora run --dry-run` to verify live output looks correct.
- [ ] Run `agora show` on an existing transcript to verify replay output looks correct.
