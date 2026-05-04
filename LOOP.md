# Closed-Loop Multi-Agent Deliberation System

## Concept
A simple, deterministic orchestration script that spins up N heterogeneous agents
(via `opencode run --model <provider/model>`).  Agents take turns in a fixed
ring topology, each able to inspect the last K messages from the previous agent.
They argue/debate a shared topic until a global timeout expires or consensus is
reached.

## ASCII Flowchart

```
                    +-------------------------+
                    |   User provides:        |
                    |   - topic / goal        |
                    |   - agent configs       |
                    |     [model1, model2,..] |
                    |   - max_turns_per_agent |
                    |   - lookback_window (K) |
                    |   - time_limit_sec      |
                    +-----------+-------------+
                                |
                                v
                    +-------------------------+
                    |  Orchestrator Script    |
                    |  (pure Python / bash)   |
                    +-----------+-------------+
                                |
           +--------------------+--------------------+
           |                    |                    |
           v                    v                    v
   +---------------+    +---------------+    +---------------+
   |   Agent 0     |    |   Agent 1     |    |   Agent N-1   |
   |  opencode run |    |  opencode run |    |  opencode run |
   | --model A/B   |    | --model C/D   |    | --model X/Y   |
   +-------+-------+    +-------+-------+    +-------+-------+
           |                    |                    |
           |  (shared message buffer / transcript)  |
           |<-------------------------------------->|
           |                    |                    |
           v                    v                    v
   Loop:  Agent i reads last K msgs from buffer
          -> composes prompt with topic + context
          -> calls `opencode run --model <cfg_i>`
          -> appends response to shared buffer
          -> hands token to Agent (i+1) % N
          -> repeat until time_limit or max_turns exhausted

   +---------------------------------------------------+
   |              Transcript (Shared Buffer)             |
   |  [turn 0] Agent0: "I think we should use Rust..." |
   |  [turn 1] Agent1: "But Go has faster compile..."  |
   |  [turn 2] Agent0: "Rebuttal: Go lacks borrow..."  |
   |  ...                                              |
   +---------------------------------------------------+
                                |
           (every handoff: orchestrator checks elapsed time)
                                |
                                v
                    +-------------------------+
                    |   Termination Check     |
                    |   time_limit reached?   |
                    |   OR max_turns reached? |
                    |   OR consensus flag?    |
                    +-----------+-------------+
                                |
                +---------------+---------------+
                | YES                           | NO
                v                               v
    +-------------------+           (continue loop)
    |  Final Synthesis  |
    |  (or simple dump  |
    |   of transcript)  |
    +---------+---------+
              |
              v
    +-------------------+
    |  Output:          |
    |  - full log       |
    |  - final summary  |
    +-------------------+
```

## Key Data Structures

### Agent Configuration
```yaml
agents:
  - id: "skeptic"
    model: "openai/gpt-4o"
    system_prompt: "You are a skeptic. Challenge every assumption."
  - id: "optimist"
    model: "anthropic/claude-3-5-sonnet"
    system_prompt: "You are an optimist. Find the best case."
```

### Shared Transcript (JSON Lines)
```json
{"turn": 0, "agent_id": "skeptic", "timestamp": 1715000000, "content": "..."}
{"turn": 1, "agent_id": "optimist", "timestamp": 1715000015, "content": "..."}
```

## Prompt Template for Each Turn
```
## Topic
{topic}

## Previous Discussion (last {lookback} messages)
{transcript_snippet}

## Your Turn
You are {agent_id} ({persona}).
Respond in character. Address the arguments above. Keep it concise.
```

## Open Questions
1.  Should the orchestrator spawn subprocesses (`opencode run`) or call an
    internal API?  Subprocesses keep it decoupled and language-agnostic.
2.  How does an agent signal "consensus"?  A structured JSON prefix in the
    response, or a separate file-based semaphore?
3.  Is the ring topology sufficient, or should we allow star / fully-connected?
    (Ring is simpler and deterministic.)
4.  Rate-limiting / cost capping: should the orchestrator pre-compute token
    budgets per agent?
