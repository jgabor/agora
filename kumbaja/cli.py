"""Click-based CLI for kumbaja."""

from __future__ import annotations

import json
import sys
import time

import click
import yaml

from .agent import AgentRunner
from .config import load_config
from .orchestrator import DeliberationState, Orchestrator
from .output import OutputManager
from .transcript import TranscriptManager
from .types import DeliberationStats, TurnRecord


@click.group()
@click.version_option(package_name="kumbaja")
def main() -> None:
    """Kumbaja — Closed-loop multi-agent deliberation system."""
    pass


@main.command()
@click.option("--config", "-c", required=True, help="Path to YAML agent configuration file")
@click.option("--topic", "-t", required=True, help="Topic or goal for deliberation")
@click.option("--time", "-T", "time_limit", type=int, required=True, help="Time limit in seconds")
@click.option("--window", "-w", type=int, required=True, help="Number of predecessor messages each agent sees")  # noqa: E501
@click.option("--max-turns", "-m", type=int, required=True, help="Maximum total turns")
@click.option("--output", "-o", required=True, help="Path to write the JSONL transcript")
@click.option("--verbose", "-v", is_flag=True, help="Print agent responses in real-time")
@click.option("--budget", type=float, default=None, help="Cost cap in dollars")
@click.option("--synthesize/--no-synthesize", default=False, help="Run final synthesis agent after deliberation")  # noqa: E501
@click.option("--full-context", is_flag=True, help="Show last K messages from ALL agents (not just predecessor)")  # noqa: E501
@click.option("--dry-run", is_flag=True, help="Run with simulated agent responses (no LLM calls)")
def run(
    config: str,
    topic: str,
    time_limit: int,
    window: int,
    max_turns: int,
    output: str,
    verbose: bool,
    budget: float | None,
    synthesize: bool,
    full_context: bool,
    dry_run: bool,
) -> None:
    """Run a multi-agent deliberation."""
    try:
        deliberation_config = load_config(config)
    except (FileNotFoundError, ValueError, yaml.YAMLError) as exc:
        click.echo(f"Error loading config: {exc}", err=True)
        sys.exit(1)

    state = DeliberationState(
        config=deliberation_config,
        topic=topic,
        window=window,
        max_turns=max_turns,
        time_limit=time_limit,
        budget=budget,
        full_context=full_context,
    )

    transcript = TranscriptManager(output)
    output_mgr = OutputManager(verbose=verbose)
    runner = AgentRunner(dry_run=dry_run)

    orchestrator = Orchestrator(
        state=state,
        transcript=transcript,
        runner=runner,
        output_callback=output_mgr,
    )

    try:
        stats = orchestrator.run()
    except KeyboardInterrupt:
        output_mgr.success("Interrupted. Partial transcript saved.")
        output_mgr.success(f"Transcript: {output}")
        sys.exit(0)

    if synthesize:
        try:
            result = orchestrator.synthesize()
            if result:
                output_mgr.success("Synthesis complete")
        except Exception as exc:
            output_mgr.error(f"Synthesis failed: {exc}")

    duration = time.time() - state.start_time

    output_mgr.success(f"Deliberation complete ({stats.total_turns} turns, {duration:.1f}s)")
    output_mgr.success(f"Transcript: {output}")
    output_mgr.info(f"Halted by: {state.halted_by or 'unknown'}")


@main.command()
@click.argument("transcript", type=click.Path(exists=True))
def stats(transcript: str) -> None:
    """Print statistics from a deliberation transcript."""
    records = _load_transcript(transcript)
    if not records:
        click.echo("Transcript empty or invalid.", err=True)
        sys.exit(1)

    stats = DeliberationStats(records)
    output_mgr = OutputManager()

    output_mgr.print_stats(stats.to_dict())


@main.command()
@click.argument("config_file", type=click.Path(exists=True))
def validate(config_file: str) -> None:
    """Validate a configuration file without running deliberation."""
    try:
        cfg = load_config(config_file)
    except Exception as exc:
        click.echo(f"ERROR: {exc}", err=True)
        sys.exit(1)

    click.echo("Configuration is valid.")
    click.echo(f"  Topology: {str(cfg.topology)}")
    click.echo(f"  Agents ({len(cfg.agents)}):")
    for agent in cfg.agents:
        click.echo(f"    - {agent.id} ({agent.model})")
    if cfg.consensus_threshold > 0:
        click.echo(f"  Consensus threshold: {cfg.consensus_threshold}")
    if cfg.synthesis_model:
        click.echo(f"  Synthesis model: {cfg.synthesis_model}")


@main.command()
@click.argument("transcript", type=click.Path(exists=True))
@click.option("--config", "-c", required=True, help="Path to YAML agent configuration file")
@click.option("--topic", "-t", required=True, help="Topic or goal for deliberation")
@click.option("--time", "-T", "time_limit", type=int, required=True, help="Additional time limit in seconds")  # noqa: E501
@click.option("--window", "-w", type=int, required=True, help="Window size")
@click.option("--max-turns", "-m", type=int, required=True, help="Additional max turns")
@click.option("--output", "-o", required=True, help="Path to write the updated JSONL transcript")
@click.option("--verbose", "-v", is_flag=True, help="Print agent responses in real-time")
@click.option("--budget", type=float, default=None, help="Remaining cost budget")
@click.option("--full-context", is_flag=True, help="Show last K messages from ALL agents (not just predecessor)")  # noqa: E501
@click.option("--dry-run", is_flag=True, help="Run with simulated agent responses (no LLM calls)")
def resume(
    transcript: str,
    config: str,
    topic: str,
    time_limit: int,
    window: int,
    max_turns: int,
    output: str,
    verbose: bool,
    budget: float | None,
    full_context: bool,
    dry_run: bool,
) -> None:
    """Continue deliberation from an existing transcript."""
    try:
        deliberation_config = load_config(config)
    except (FileNotFoundError, ValueError, yaml.YAMLError) as exc:
        click.echo(f"Error loading config: {exc}", err=True)
        sys.exit(1)

    # Load existing transcript
    tm = TranscriptManager(output)
    existing = tm.load_existing()
    if len(existing) == 0:
        click.echo("No existing transcript found. Use 'kumbaja run' to start.", err=True)
        sys.exit(1)

    loaded = _load_transcript(transcript)
    if not loaded:
        click.echo("Transcript empty or invalid.", err=True)
        sys.exit(1)

    # Copy loaded records into the manager
    for record in loaded:
        tm._records.append(record)
    tm._written = len(tm._records)
    tm.write_all()

    # Determine starting turn
    existing_turns = len([r for r in loaded if r.agent_id != "orchestrator"])

    state = DeliberationState(
        config=deliberation_config,
        topic=topic,
        window=window,
        max_turns=existing_turns + max_turns,
        time_limit=time_limit,
        budget=budget,
        full_context=full_context,
    )
    state.turn = existing_turns

    output_mgr = OutputManager(verbose=verbose)
    runner = AgentRunner(dry_run=dry_run)

    orchestrator = Orchestrator(
        state=state,
        transcript=tm,
        runner=runner,
        output_callback=output_mgr,
    )

    try:
        stats = orchestrator.run()
    except KeyboardInterrupt:
        output_mgr.success("Interrupted. Partial transcript saved.")
        output_mgr.success(f"Transcript: {output}")
        sys.exit(0)

    output_mgr.success(f"Resumed deliberation complete ({stats.total_turns} total turns)")
    output_mgr.success(f"Transcript: {output}")


def _load_transcript(path: str) -> list[TurnRecord]:
    records = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                record = TurnRecord.from_json(line)
                records.append(record)
            except (json.JSONDecodeError, KeyError):
                continue
    return records
