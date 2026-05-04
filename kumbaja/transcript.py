"""Transcript reading, writing, and management."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from .types import TurnRecord


class TranscriptManager:
    """Manages the deliberation transcript as a JSONL file."""

    def __init__(self, path: str) -> None:
        self.path = Path(path)
        self._records: list[TurnRecord] = []
        self._written = 0

    @property
    def records(self) -> list[TurnRecord]:
        return self._records

    def load_existing(self) -> list[TurnRecord]:
        """Load an existing transcript file for resume."""
        if not self.path.exists():
            return []
        loaded: list[TurnRecord] = []
        with open(self.path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    record = TurnRecord.from_json(line)
                    loaded.append(record)
                except (json.JSONDecodeError, KeyError):
                    continue
        self._records = loaded
        self._written = len(loaded)
        return loaded

    def append(self, record: TurnRecord) -> None:
        """Append a single record and write to disk."""
        self._records.append(record)
        # Write all unwritten records
        with open(self.path, "a") as f:
            for i in range(self._written, len(self._records)):
                f.write(self._records[i].to_json() + "\n")
            self._written = len(self._records)

    def write_all(self) -> None:
        """Rewrite the entire transcript file from memory."""
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with open(self.path, "w") as f:
            for record in self._records:
                f.write(record.to_json() + "\n")
        self._written = len(self._records)

    def last_agent_turn_index(self, agent_id: str) -> int | None:
        """Return the most recent turn index for a given agent, or None."""
        for i in range(len(self._records) - 1, -1, -1):
            if self._records[i].agent_id == agent_id:
                return i
        return None

    def history_for_agent(
        self,
        _agent_id: str,
        window: int,
        topology: Any,
        num_agents: int,
        turn: int,
    ) -> list[dict[str, str]]:
        """Build the history envelope for the next agent turn."""
        from .types import Topology

        if topology == Topology.STAR:
            # Star: see last K messages from any agent
            return [
                {"agent_id": r.agent_id, "content": r.content}
                for r in self._records[-window:]
            ]
        elif topology == Topology.MESH:
            # Mesh: see last K messages from any agent (same as star for envelope)
            return [
                {"agent_id": r.agent_id, "content": r.content}
                for r in self._records[-window:]
            ]
        else:
            # Ring: see last K messages from the predecessor agent
            if turn == 0:
                predecessor_id = "orchestrator"
            else:
                predecessor_idx = (turn - 1) % num_agents
                # Find the agent config for this index
                agent_order = self._infer_agent_order(num_agents)
                predecessor_id = agent_order[predecessor_idx]

            history = []
            for msg in reversed(self._records):
                if msg.agent_id == predecessor_id:
                    history.append({"agent_id": msg.agent_id, "content": msg.content})
                if len(history) >= window:
                    break
            history.reverse()
            return history

    def _infer_agent_order(self, num_agents: int) -> list[str]:
        """Infer the agent order from the first N non-orchestrator turns."""
        seen: list[str] = []
        for r in self._records:
            if r.agent_id == "orchestrator":
                continue
            if r.agent_id not in seen:
                seen.append(r.agent_id)
            if len(seen) >= num_agents:
                break
        return seen

    def consecutive_consensus_count(self) -> int:
        """Count how many consecutive turns have consensus markers."""
        count = 0
        for r in reversed(self._records):
            if r.consensus:
                count += 1
            else:
                break
        return count

    def total_cost(self) -> float:
        return sum(r.cost for r in self._records if r.cost is not None)

    def total_tokens(self) -> int:
        total = 0
        for r in self._records:
            if r.tokens.total:
                total += r.tokens.total
        return total
