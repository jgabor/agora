"""Core deliberation orchestrator."""

from __future__ import annotations

import signal
import time
from typing import Any

from .agent import AgentRunner, extract_consensus
from .output import OutputManager
from .synthesis import SynthesisEngine
from .transcript import TranscriptManager
from .types import (
    AgentConfig,
    DeliberationConfig,
    DeliberationStats,
    TokenUsage,
    TurnRecord,
)


class DeliberationState:
    """Tracks the runtime state of an ongoing deliberation."""

    def __init__(
        self,
        config: DeliberationConfig,
        topic: str,
        window: int,
        max_turns: int,
        time_limit: int,
        budget: float | None,
        full_context: bool,
    ) -> None:
        self.config = config
        self.topic = topic
        self.window = window
        self.max_turns = max_turns
        self.time_limit = time_limit
        self.budget = budget
        self.full_context = full_context

        self.turn: int = 0
        self.start_time: float = 0.0
        self.running: bool = True
        self.halted_by: str = ""


class Orchestrator:
    """Orchestrates multi-agent deliberation."""

    def __init__(
        self,
        state: DeliberationState,
        transcript: TranscriptManager,
        runner: AgentRunner,
        output_callback: OutputManager | None = None,
    ) -> None:
        self.state = state
        self.transcript = transcript
        self.runner = runner
        self.output = output_callback

        self._num_agents = len(state.config.agents)
        self._consensus_streak = 0

    def run(self) -> DeliberationStats:
        """Execute the full deliberation. Returns stats."""
        self.state.running = True
        self.state.start_time = time.time()

        self._setup_signal_handlers()

        # Seed message (unless resuming)
        if len(self.transcript.records) == 0:
            self._emit_seed()

        if self.output:
            self.output.deliberation_header(self.state)

        try:
            while self.state.running and self.state.turn < self.state.max_turns:
                self._check_termination_conditions()
                if not self.state.running:
                    break

                agent_idx = self.state.turn % self._num_agents
                agent = self.state.config.agents[agent_idx]

                turn_record = self._execute_turn(agent)
                self.transcript.append(turn_record)
                self._consensus_streak = self.transcript.consecutive_consensus_count()

                if self.output:
                    self.output.turn_progress(turn_record, self.state)

                self.state.turn += 1

            else:
                if self.state.turn >= self.state.max_turns:
                    self.state.halted_by = f"max_turns ({self.state.max_turns})"
                    if self.output:
                        self.output.delimiter()
                        self.output.info(f"Max turns ({self.state.max_turns}) reached.")

        except RuntimeError as exc:
            self.state.halted_by = f"error: {exc}"
            if self.output:
                self.output.error(str(exc))
                self.output.info("Writing partial transcript...")

        if self.output:
            self.output.delimiter()
            self.output.final_stats(self.transcript.records, self.state)

        self.transcript.write_all()
        return DeliberationStats(self.transcript.records)

    def synthesize(self) -> dict[str, Any] | None:
        """Run final synthesis. Called after deliberation completes."""
        if len(self.transcript.records) <= 1:
            return None

        if self.output:
            self.output.info("Running final synthesis...")

        engine = SynthesisEngine(self.runner)
        result = engine.synthesize(
            self.transcript.records,
            self.state.topic,
            self.state.config,
        )

        if self.output:
            self.output.synthesis_result(result)

        return result

    def _emit_seed(self) -> None:
        seed = TurnRecord(
            turn=-1,
            agent_id="orchestrator",
            model=None,
            timestamp=time.time(),
            content=f"Begin deliberating on the following topic: {self.state.topic}",
            elapsed=0.0,
        )
        self.transcript.append(seed)

    def _check_termination_conditions(self) -> None:
        elapsed = time.time() - self.state.start_time

        if elapsed >= self.state.time_limit:
            self.state.running = False
            self.state.halted_by = f"time_limit ({self.state.time_limit}s)"
            if self.output:
                self.output.info("Time limit reached.")
            return

        if (
            self.state.config.consensus_threshold > 0
            and self._consensus_streak >= self.state.config.consensus_threshold
        ):
            self.state.running = False
            self.state.halted_by = f"consensus ({self._consensus_streak} consecutive agreements)"
            if self.output:
                self.output.info(
                    f"Consensus reached ({self._consensus_streak} consecutive agreements)."
                )
            return

        if self.state.budget is not None and self.transcript.total_cost() >= self.state.budget:
            self.state.running = False
            self.state.halted_by = f"budget_exceeded (${self.state.budget:.2f})"
            if self.output:
                self.output.info(
                    f"Budget exceeded (${self.transcript.total_cost():.6f} "
                f">= ${self.state.budget:.2f})."
                )
            return

    def _execute_turn(self, agent: AgentConfig) -> TurnRecord:
        turn_start = time.time()

        # Build history envelope
        history = self.transcript.history_for_agent(
            agent.id,
            self.state.window,
            self.state.config.topology,
            self._num_agents,
            self.state.turn,
        )

        envelope: dict[str, Any] = {
            "topic": self.state.topic,
            "history": history,
        }

        if self.state.full_context:
            # Override history to include last K messages from ANY agent
            envelope["history"] = [
                {"agent_id": r.agent_id, "content": r.content}
                for r in self.transcript.records[-self.state.window :]
            ]

        content, metadata = self.runner.run(agent, envelope)

        # Extract consensus
        cleaned_content, has_consensus, consensus_stmt = extract_consensus(content)

        tokens = TokenUsage.from_dict(metadata.get("tokens", {}))
        cost = metadata.get("cost")
        turn_duration = time.time() - turn_start

        return TurnRecord(
            turn=self.state.turn,
            agent_id=agent.id,
            model=agent.model,
            timestamp=time.time(),
            content=cleaned_content,
            tokens=tokens,
            cost=cost,
            consensus=has_consensus,
            consensus_statement=consensus_stmt,
            elapsed=turn_duration,
        )

    def _setup_signal_handlers(self) -> None:
        orig_sigint = signal.getsignal(signal.SIGINT)
        orig_sigterm = signal.getsignal(signal.SIGTERM)

        def _handler(_signum: int, _frame: Any) -> None:
            self.state.running = False
            self.state.halted_by = "user_interrupt"
            self.transcript.write_all()
            if self.output:
                self.output.info(
                    "Interrupted. Partial transcript saved."
                )
            # Restore original handler and raise again
            signal.signal(signal.SIGINT, orig_sigint)
            signal.signal(signal.SIGTERM, orig_sigterm)
            raise KeyboardInterrupt

        try:
            signal.signal(signal.SIGINT, _handler)
            signal.signal(signal.SIGTERM, _handler)
        except ValueError:
            # Signal handlers can only be set in the main thread
            pass
