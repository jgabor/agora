"""Tests for config loading and validation."""

from __future__ import annotations

import tempfile
from pathlib import Path

import pytest
import yaml

from kumbaja.config import load_config
from kumbaja.types import Topology


def test_load_valid_config():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {
                "agents": [
                    {"id": "agent1", "model": "openai/gpt-4", "system_prompt": "Be helpful"}
                ]
            },
            f,
        )
    try:
        cfg = load_config(f.name)
        assert len(cfg.agents) == 1
        assert cfg.agents[0].id == "agent1"
        assert cfg.agents[0].model == "openai/gpt-4"
        assert cfg.topology == Topology.RING
    finally:
        Path(f.name).unlink()


def test_load_config_with_topology():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {
                "topology": "star",
                "agents": [{"id": "a", "model": "m"}],
            },
            f,
        )
    try:
        cfg = load_config(f.name)
        assert cfg.topology == Topology.STAR
    finally:
        Path(f.name).unlink()


def test_load_config_with_topology_mesh():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {
                "topology": "mesh",
                "agents": [{"id": "a", "model": "m"}],
            },
            f,
        )
    try:
        cfg = load_config(f.name)
        assert cfg.topology == Topology.MESH
    finally:
        Path(f.name).unlink()


def test_load_config_with_consensus():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {
                "consensus_threshold": 3,
                "agents": [{"id": "a", "model": "m"}],
            },
            f,
        )
    try:
        cfg = load_config(f.name)
        assert cfg.consensus_threshold == 3
    finally:
        Path(f.name).unlink()


def test_load_config_with_synthesis_model():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {
                "synthesis_model": "openai/gpt-4",
                "agents": [{"id": "a", "model": "m"}],
            },
            f,
        )
    try:
        cfg = load_config(f.name)
        assert cfg.synthesis_model == "openai/gpt-4"
    finally:
        Path(f.name).unlink()


def test_load_config_file_not_found():
    with pytest.raises(FileNotFoundError):
        load_config("/nonexistent/path/config.yaml")


def test_load_config_no_agents():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump({"agents": []}, f)
    try:
        with pytest.raises(ValueError, match="at least one agent"):
            load_config(f.name)
    finally:
        Path(f.name).unlink()


def test_load_config_duplicate_ids():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {
                "agents": [
                    {"id": "dup", "model": "m1"},
                    {"id": "dup", "model": "m2"},
                ]
            },
            f,
        )
    try:
        with pytest.raises(ValueError, match="Duplicate"):
            load_config(f.name)
    finally:
        Path(f.name).unlink()


def test_load_config_missing_id():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {"agents": [{"model": "m"}]},
            f,
        )
    try:
        with pytest.raises(ValueError, match="non-empty 'id'"):
            load_config(f.name)
    finally:
        Path(f.name).unlink()


def test_load_config_missing_model():
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".yaml", delete=False
    ) as f:
        yaml.dump(
            {"agents": [{"id": "a"}]},
            f,
        )
    try:
        with pytest.raises(ValueError, match="non-empty 'model'"):
            load_config(f.name)
    finally:
        Path(f.name).unlink()


def test_topology_from_str_valid():
    assert Topology.from_str("ring") == Topology.RING
    assert Topology.from_str("RING") == Topology.RING
    assert Topology.from_str("star") == Topology.STAR
    assert Topology.from_str("mesh") == Topology.MESH


def test_topology_from_str_invalid():
    with pytest.raises(ValueError, match="Unknown topology"):
        Topology.from_str("invalid")
