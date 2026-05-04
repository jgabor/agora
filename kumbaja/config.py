"""Configuration loading and validation."""

from __future__ import annotations

from pathlib import Path

import yaml

from .types import AgentConfig, DeliberationConfig, Topology


def load_config(path: str) -> DeliberationConfig:
    """Load and validate a deliberation configuration from a YAML file."""
    config_path = Path(path)
    if not config_path.exists():
        raise FileNotFoundError(f"Config file not found: {path}")

    with open(config_path) as f:
        data = yaml.safe_load(f)

    if not isinstance(data, dict):
        raise ValueError("Config file must contain a top-level mapping")

    topology = Topology.RING
    if "topology" in data:
        topology = Topology.from_str(str(data["topology"]))

    consensus_threshold = data.get("consensus_threshold", 0)
    synthesis_model = data.get("synthesis_model")

    raw_agents = data.get("agents", [])
    if not raw_agents:
        raise ValueError("Configuration must contain at least one agent")

    agents: list[AgentConfig] = []
    for i, raw in enumerate(raw_agents):
        if not isinstance(raw, dict):
            raise ValueError(f"Agent {i} must be a mapping")
        agent = AgentConfig(
            id=str(raw.get("id", "")),
            model=str(raw.get("model", "")),
            system_prompt=str(raw.get("system_prompt", "")),
        )
        agents.append(agent)

    config = DeliberationConfig(
        agents=agents,
        topology=topology,
        consensus_threshold=consensus_threshold,
        synthesis_model=synthesis_model,
    )
    config.validate()
    return config



