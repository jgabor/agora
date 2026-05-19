package cast

import (
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestCastNew(t *testing.T) {
	agents := []types.AgentConfig{
		{ID: "agent1", Model: "model1"},
		{ID: "agent2", Model: "model2"},
	}
	c := New(agents)

	if len(c.Members()) != 2 {
		t.Fatalf("expected 2 members, got %d", len(c.Members()))
	}

	m1 := c.Profile("agent1")
	if m1.Name != "Solon" || m1.Color != "6" {
		t.Errorf("agent1: expected Solon/6, got %s/%s", m1.Name, m1.Color)
	}

	m2 := c.Profile("agent2")
	if m2.Name != "Aspasia" || m2.Color != "4" {
		t.Errorf("agent2: expected Aspasia/4, got %s/%s", m2.Name, m2.Color)
	}
}

func TestCastFromMetadata(t *testing.T) {
	meta := &types.TranscriptMetadata{
		Cast: []types.CastMember{
			{ID: 1, Name: "CustomName", Persona: "agent1", Color: "1"},
		},
	}
	c := FromMetadata(meta)

	m := c.Profile("agent1")
	if m.Name != "CustomName" || m.Color != "1" {
		t.Errorf("expected CustomName/1, got %s/%s", m.Name, m.Color)
	}
}

func TestCastBadge(t *testing.T) {
	agents := []types.AgentConfig{{ID: "strategist"}}
	c := New(agents)

	if b := c.Badge("strategist"); b != "[A1 strategist]" {
		t.Errorf("expected [A1 strategist], got %s", b)
	}

	if b := c.Badge("unknown"); b != "[A? unknown]" {
		t.Errorf("expected [A? unknown], got %s", b)
	}
}

func TestCastColor(t *testing.T) {
	agents := []types.AgentConfig{{ID: "strategist"}}
	c := New(agents)

	if col := c.Color("strategist"); col != "6" {
		t.Errorf("expected color 6, got %s", col)
	}

	if col := c.Color("orchestrator"); col != "6" {
		t.Errorf("expected orchestrator color 6, got %s", col)
	}
}
