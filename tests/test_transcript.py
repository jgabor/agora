"""Tests for TurnRecord, transcript, and stats."""

from __future__ import annotations

import json
import tempfile
import time
from pathlib import Path

from kumbaja.types import (
    DeliberationStats,
    TokenUsage,
    Topology,
    TurnRecord,
)
from kumbaja.transcript import TranscriptManager


def test_turn_record_serialization():
    record = TurnRecord(
        turn=0,
        agent_id="skeptic",
        model="openai/gpt-4",
        timestamp=1715000000.0,
        content="Hello world",
        tokens=TokenUsage(total=100, input=60, output=40),
        cost=0.001,
        consensus=True,
        consensus_statement="We agree",
        elapsed=2.5,
    )

    json_str = record.to_json()
    parsed = json.loads(json_str)
    assert parsed["turn"] == 0
    assert parsed["agent_id"] == "skeptic"
    assert parsed["content"] == "Hello world"
    assert parsed["tokens"]["total"] == 100
    assert parsed["cost"] == 0.001
    assert parsed["consensus"] is True
    assert parsed["consensus_statement"] == "We agree"
    assert parsed["elapsed"] == 2.5


def test_turn_record_deserialization():
    data = {
        "turn": 1,
        "agent_id": "optimist",
        "model": "anthropic/claude-3",
        "timestamp": 1715000010.0,
        "content": "Response text",
        "tokens": {"total": 200, "input": 100, "output": 100},
        "cost": 0.002,
        "consensus": False,
        "consensus_statement": "",
        "elapsed": 3.0,
    }
    record = TurnRecord.from_dict(data)
    assert record.turn == 1
    assert record.agent_id == "optimist"
    assert record.content == "Response text"
    assert record.tokens.total == 200
    assert record.cost == 0.002
    assert record.elapsed == 3.0


def test_transcript_manager_append():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".jsonl", delete=False
    ) as f:
        pass
    try:
        tm = TranscriptManager(f.name)
        record = TurnRecord(
            turn=0,
            agent_id="test",
            model="m",
            timestamp=time.time(),
            content="test",
        )
        tm.append(record)
        assert len(tm.records) == 1
        assert tm.records[0].agent_id == "test"
    finally:
        Path(f.name).unlink()


def test_transcript_manager_load_and_resume():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".jsonl", delete=False
    ) as f:
        pass

    try:
        tm = TranscriptManager(f.name)
        tm.append(
            TurnRecord(
                turn=-1,
                agent_id="orchestrator",
                model=None,
                timestamp=time.time(),
                content="Seed",
            )
        )
        tm.append(
            TurnRecord(
                turn=0,
                agent_id="agent1",
                model="m",
                timestamp=time.time(),
                content="Turn 0",
            )
        )

        tm2 = TranscriptManager(f.name)
        loaded = tm2.load_existing()
        assert len(loaded) == 2
        assert loaded[0].agent_id == "orchestrator"
        assert loaded[1].agent_id == "agent1"
    finally:
        Path(f.name).unlink()


def test_transcript_total_cost():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".jsonl", delete=False
    ) as f:
        pass

    try:
        tm = TranscriptManager(f.name)
        tm.append(
            TurnRecord(
                turn=0, agent_id="a", model="m", timestamp=1.0, content="x", cost=0.001
            )
        )
        tm.append(
            TurnRecord(
                turn=1, agent_id="a", model="m", timestamp=2.0, content="x", cost=0.002
            )
        )
        assert tm.total_cost() == 0.003
    finally:
        Path(f.name).unlink()


def test_consecutive_consensus_count():
    records = []

    tm = _make_empty_transcript()
    tm.append(
        TurnRecord(
            turn=0, agent_id="a", model="m", timestamp=1.0, content="x", consensus=True
        )
    )
    tm.append(
        TurnRecord(
            turn=1, agent_id="b", model="m", timestamp=2.0, content="x", consensus=True
        )
    )
    assert tm.consecutive_consensus_count() == 2

    tm.append(
        TurnRecord(
            turn=2, agent_id="c", model="m", timestamp=3.0, content="x", consensus=False
        )
    )
    assert tm.consecutive_consensus_count() == 0


def test_deliberation_stats():
    records = [
        TurnRecord(
            turn=0, agent_id="a", model="m", timestamp=1.0, content="x",
            tokens=TokenUsage(total=100), cost=0.001,
        ),
        TurnRecord(
            turn=1, agent_id="b", model="m", timestamp=2.0, content="x",
            tokens=TokenUsage(total=200), cost=0.002,
        ),
        TurnRecord(
            turn=2, agent_id="a", model="m", timestamp=3.0, content="x",
            tokens=TokenUsage(total=50), cost=0.0005,
            consensus=True, consensus_statement="ok",
        ),
    ]
    stats = DeliberationStats(records)
    assert stats.total_turns == 3
    assert stats.total_tokens == 350
    assert abs(stats.total_cost - 0.0035) < 1e-10

    per_agent = stats.per_agent_stats()
    assert per_agent["a"]["turns"] == 2
    assert per_agent["b"]["turns"] == 1

    consensus = stats.consensus_events()
    assert len(consensus) == 1
    assert consensus[0]["statement"] == "ok"


def test_stats_to_dict():
    records = [
        TurnRecord(
            turn=0, agent_id="a", model="m", timestamp=1.0, content="x",
            tokens=TokenUsage(total=100), cost=0.002,
        ),
    ]
    stats = DeliberationStats(records)
    d = stats.to_dict()
    assert d["total_turns"] == 1
    assert d["total_tokens"] == 100
    assert d["total_cost"] == 0.002
    assert "per_agent" in d


def test_history_ring_topology():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".jsonl", delete=False
    ) as f:
        pass
    try:
        tm = TranscriptManager(f.name)
        tm.append(
            TurnRecord(
                turn=-1,
                agent_id="orchestrator",
                model=None,
                timestamp=time.time(),
                content="seed",
            )
        )
        tm.append(
            TurnRecord(
                turn=0, agent_id="a", model="m", timestamp=time.time(), content="msg a"
            )
        )

        history = tm.history_for_agent("b", 5, Topology.RING, 2, 1)
        assert len(history) == 1
        assert history[0]["agent_id"] == "a"
    finally:
        Path(f.name).unlink()


def test_history_star_topology():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".jsonl", delete=False
    ) as f:
        pass
    try:
        tm = TranscriptManager(f.name)
        tm.append(
            TurnRecord(
                turn=-1, agent_id="orchestrator", model=None, timestamp=time.time(), content="s"
            )
        )
        tm.append(
            TurnRecord(
                turn=0, agent_id="a", model="m", timestamp=time.time(), content="a"
            )
        )
        tm.append(
            TurnRecord(
                turn=1, agent_id="b", model="m", timestamp=time.time(), content="b"
            )
        )

        history = tm.history_for_agent("c", 3, Topology.STAR, 2, 2)
        assert len(history) == 3
    finally:
        Path(f.name).unlink()


def _make_empty_transcript() -> TranscriptManager:
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".jsonl", delete=False
    ) as f:
        pass
    tm = TranscriptManager(f.name)
    return tm
