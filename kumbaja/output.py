"""Terminal output and formatting using rich."""

from __future__ import annotations

import time
from typing import Any

from rich.console import Console
from rich.panel import Panel
from rich.table import Table

from .types import DeliberationStats, TurnRecord


class OutputManager:
    """Manages terminal output for deliberation progress."""

    def __init__(self, verbose: bool = False) -> None:
        self.console = Console(highlight=False)
        self.verbose = verbose

    HEADER_COLORS = {
        "orchestrator": "cyan",
        "strategist": "blue",
        "domain_expert": "green",
        "domain-expert": "green",
        "skeptic": "red",
        "optimist": "yellow",
        "user_advocate": "magenta",
        "user-advocate": "magenta",
        "implementer": "bright_black",
        "risk_officer": "bright_red",
        "risk-officer": "bright_red",
        "synthesizer": "bright_cyan",
    }

    def deliberation_header(self, state: Any) -> None:
        """Print the deliberation start banner."""

        self.console.print()
        self.console.print(
            Panel.fit(
                state.topic,
                title="[bold]Topic[/]",
                border_style="blue",
            )
        )

        agents_str = ", ".join(a.id for a in state.config.agents)
        self.console.print(f"[bold]Agents:[/] {agents_str}")
        self.console.print(
            f"[bold]Settings:[/] topology={str(state.config.topology)} | "
            f"time={state.time_limit}s | max_turns={state.max_turns} | "
            f"window={state.window}"
            f"{' | budget=$' + str(state.budget) if state.budget else ''}"
            f"{' | consensus_threshold=' + str(state.config.consensus_threshold) if state.config.consensus_threshold > 0 else ''}"  # noqa: E501
        )
        self.console.print()

    def turn_progress(self, record: TurnRecord, state: Any) -> None:
        """Print progress for a single turn."""
        elapsed = f"{record.elapsed:.1f}s"
        tokens_total = record.tokens.total if record.tokens.total else "?"
        cost_str = f"${record.cost:.6f}" if record.cost is not None else "?"

        color = self.HEADER_COLORS.get(record.agent_id, "white")
        agent_display = f"[{color}]{record.agent_id}[/{color}]"
        model_display = f"[dim]{record.model or ''}[/dim]"

        self.console.print(
            f"[{record.turn + 1}/{state.max_turns}] {agent_display} "
            f"{model_display} "
            f"[dim]· {elapsed} · {tokens_total}tok · {cost_str}[/dim]"
        )

        if record.consensus:
            self.console.print(
                f"  [green bold]✓ CONSENSUS:[/] {record.consensus_statement}"
            )

        if self.verbose and record.content:
            self.console.print()
            for line in record.content.splitlines():
                self.console.print(f"  [dim]│[/] {line}")
            self.console.print()

    def synthesize_header(self) -> None:
        self.console.print()
        self.console.print("[bold]Synthesis:[/]")

    def synthesis_result(self, result: dict[str, Any]) -> None:
        """Display synthesis results in a formatted table/pane."""
        self.console.print()

        if result.get("recommended_decision"):
            self.console.print(
                Panel(
                    result["recommended_decision"],
                    title="[bold]Recommended Decision[/]",
                    border_style="green",
                )
            )

        confidence = result.get("confidence", "?")
        conf_color = {"high": "green", "medium": "yellow", "low": "red"}.get(confidence, "white")
        self.console.print(f"[bold]Confidence:[/] [{conf_color}]{confidence}[/{conf_color}]")

        if result.get("key_arguments"):
            self.console.print("\n[bold]Key Arguments:[/]")
            for arg in result["key_arguments"]:
                self.console.print(f"  [dim]•[/] {arg}")

        if result.get("points_of_agreement"):
            self.console.print("\n[bold green]Points of Agreement:[/]")
            for pt in result["points_of_agreement"]:
                self.console.print(f"  [green]✓[/] {pt}")

        if result.get("unresolved_tensions"):
            self.console.print("\n[bold yellow]Unresolved Tensions:[/]")
            for t in result["unresolved_tensions"]:
                self.console.print(f"  [yellow]⚡[/] {t}")

    def final_stats(self, records: list[TurnRecord], state: Any) -> None:
        """Print final statistics."""
        stats = DeliberationStats(records)
        duration = time.time() - state.start_time

        table = Table(title="Deliberation Summary")
        table.add_column("Metric", style="cyan")
        table.add_column("Value", style="white")

        actual_turns = len([r for r in records if r.agent_id != "orchestrator"])
        table.add_row("Turns completed", str(actual_turns))
        table.add_row("Duration", f"{duration:.1f}s")
        table.add_row("Total tokens", str(stats.total_tokens))
        table.add_row("Total cost", f"${stats.total_cost:.6f}")
        table.add_row("Halted by", state.halted_by or "unknown")

        self.console.print()
        self.console.print(table)

        per_agent = stats.per_agent_stats()
        if per_agent:
            self.console.print()
            agent_table = Table(title="Per-Agent Stats")
            agent_table.add_column("Agent", style="cyan")
            agent_table.add_column("Turns", justify="right")
            agent_table.add_column("Tokens", justify="right")
            agent_table.add_column("Cost", justify="right")

            for agent_id, s in per_agent.items():
                agent_table.add_row(
                    agent_id,
                    str(s["turns"]),
                    str(s["tokens"]),
                    f"${s['cost']:.6f}",
                )

            self.console.print(agent_table)

    def print_stats(self, stats_dict: dict[str, Any]) -> None:
        """Display stats from a standalone stats command."""
        self.console.print()

        table = Table(title="Transcript Statistics")
        table.add_column("Metric", style="cyan")
        table.add_column("Value", style="white")

        table.add_row("Total turns", str(stats_dict.get("total_turns", 0)))
        table.add_row("Total tokens", str(stats_dict.get("total_tokens", 0)))
        table.add_row("Total cost", f"${stats_dict.get('total_cost', 0):.6f}")
        table.add_row("Avg turn duration", f"{stats_dict.get('avg_turn_duration_seconds', 0)}s")

        self.console.print(table)

        per_agent = stats_dict.get("per_agent", {})
        if per_agent:
            self.console.print()
            agent_table = Table(title="Per-Agent Stats")
            agent_table.add_column("Agent", style="cyan")
            agent_table.add_column("Turns", justify="right")
            agent_table.add_column("Tokens", justify="right")
            agent_table.add_column("Cost", justify="right")

            for agent_id, s in per_agent.items():
                agent_table.add_row(
                    agent_id,
                    str(s["turns"]),
                    str(s["tokens"]),
                    f"${s['cost']:.6f}",
                )

            self.console.print(agent_table)

        consensus_events = stats_dict.get("consensus_events", [])
        if consensus_events:
            self.console.print()
            self.console.print("[bold]Consensus Events:[/]")
            for evt in consensus_events:
                self.console.print(
                    f"  Turn {evt['turn']} [{evt['agent_id']}]: {evt['statement']}"
                )

    def info(self, message: str) -> None:
        self.console.print(f"[bold blue]ℹ[/] {message}")

    def error(self, message: str) -> None:
        self.console.print(f"[bold red]✗[/] {message}")

    def success(self, message: str) -> None:
        self.console.print(f"[bold green]✓[/] {message}")

    def delimiter(self) -> None:
        self.console.print("[dim]" + "─" * 60 + "[/dim]")
