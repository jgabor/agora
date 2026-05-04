"""Core data types for the kumbaja deliberation system."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from enum import Enum, auto
from typing import Any


class Topology(Enum):
    """Agent interaction topologies."""

    RING = auto()
    STAR = auto()
    MESH = auto()

    @classmethod
    def from_str(cls, value: str) -> Topology:
        try:
            return cls[value.upper().replace("-", "_")]
        except KeyError as exc:
            valid = ", ".join(t.name.lower() for t in cls)
            raise ValueError(
                f"Unknown topology '{value}'. Expected one of: {valid}"
            ) from exc

    def __str__(self) -> str:
        return self.name.lower()


@dataclass
class AgentConfig:
    """Configuration for a single deliberation agent."""

    id: str
    model: str
    system_prompt: str = ""

    def validate(self) -> None:
        if not self.id:
            raise ValueError("Agent must have a non-empty 'id'")
        if not self.model:
            raise ValueError(f"Agent '{self.id}' must have a non-empty 'model'")


@dataclass
class DeliberationConfig:
    """Top-level deliberation configuration."""

    agents: list[AgentConfig]
    topology: Topology = Topology.RING
    consensus_threshold: int = 0  # 0 means disabled
    synthesis_model: str | None = None

    def validate(self) -> None:
        if not self.agents:
            raise ValueError("Configuration must contain at least one agent")
        seen_ids = set()
        for _i, agent in enumerate(self.agents):
            agent.validate()
            if agent.id in seen_ids:
                raise ValueError(f"Duplicate agent id: '{agent.id}'")
            seen_ids.add(agent.id)
        if self.consensus_threshold < 0:
            raise ValueError("consensus_threshold must be >= 0")


@dataclass
class TokenUsage:
    """Token usage metadata from a model call."""

    total: int | None = None
    input: int | None = None
    output: int | None = None
    reasoning: int | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> TokenUsage:
        return cls(
            total=data.get("total"),
            input=data.get("input"),
            output=data.get("output"),
            reasoning=data.get("reasoning"),
        )

    def to_dict(self) -> dict[str, Any]:
        return {
            k: v
            for k, v in {
                "total": self.total,
                "input": self.input,
                "output": self.output,
                "reasoning": self.reasoning,
            }.items()
            if v is not None
        }


@dataclass
class TurnRecord:
    """A single turn in the deliberation transcript."""

    turn: int
    agent_id: str
    model: str | None
    timestamp: float
    content: str
    tokens: TokenUsage = field(default_factory=TokenUsage)
    cost: float | None = None
    consensus: bool = False
    consensus_statement: str = ""
    elapsed: float = 0.0

    def to_dict(self) -> dict[str, Any]:
        return {
            "turn": self.turn,
            "agent_id": self.agent_id,
            "model": self.model,
            "timestamp": self.timestamp,
            "content": self.content,
            "tokens": self.tokens.to_dict(),
            "cost": self.cost,
            "consensus": self.consensus,
            "consensus_statement": self.consensus_statement,
            "elapsed": self.elapsed,
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> TurnRecord:
        return cls(
            turn=data["turn"],
            agent_id=data["agent_id"],
            model=data.get("model"),
            timestamp=data["timestamp"],
            content=data["content"],
            tokens=TokenUsage.from_dict(data.get("tokens", {})),
            cost=data.get("cost"),
            consensus=data.get("consensus", False),
            consensus_statement=data.get("consensus_statement", ""),
            elapsed=data.get("elapsed", 0.0),
        )

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), ensure_ascii=False)

    @classmethod
    def from_json(cls, line: str) -> TurnRecord:
        return cls.from_dict(json.loads(line))


class DeliberationStats:
    """Statistics computed from a transcript."""

    def __init__(self, records: list[TurnRecord]) -> None:
        self.records = records

    @property
    def total_turns(self) -> int:
        return len(self.records)

    @property
    def total_tokens(self) -> int:
        total = 0
        for r in self.records:
            if r.tokens.total:
                total += r.tokens.total
        return total

    @property
    def total_cost(self) -> float:
        return sum(r.cost for r in self.records if r.cost is not None)

    @property
    def avg_turn_duration(self) -> float:
        durations = [r.elapsed for r in self.records if r.elapsed > 0]
        if not durations:
            return 0.0
        return sum(durations) / len(durations)

    def per_agent_stats(self) -> dict[str, dict[str, Any]]:
        stats: dict[str, dict[str, Any]] = {}
        for r in self.records:
            sid = r.agent_id
            if sid not in stats:
                stats[sid] = {"turns": 0, "tokens": 0, "cost": 0.0}
            stats[sid]["turns"] += 1
            if r.tokens.total:
                stats[sid]["tokens"] += r.tokens.total
            if r.cost is not None:
                stats[sid]["cost"] += r.cost
        return stats

    def consensus_events(self) -> list[dict[str, Any]]:
        events = []
        for r in self.records:
            if r.consensus:
                events.append({
                    "turn": r.turn,
                    "agent_id": r.agent_id,
                    "statement": r.consensus_statement,
                })
        return events

    def to_dict(self) -> dict[str, Any]:
        per_agent = self.per_agent_stats()
        return {
            "total_turns": self.total_turns,
            "total_tokens": self.total_tokens,
            "total_cost": round(self.total_cost, 6),
            "avg_turn_duration_seconds": round(self.avg_turn_duration, 1),
            "per_agent": {
                k: {"turns": v["turns"], "tokens": v["tokens"], "cost": round(v["cost"], 6)}
                for k, v in per_agent.items()
            },
            "consensus_events": self.consensus_events(),
        }
