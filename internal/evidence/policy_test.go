package evidence

import (
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestResolveRequestPrecedence(t *testing.T) {
	falseValue := false
	cfg := &types.DeliberationConfig{
		ResearchEnabled: true,
		ContextPaths:    []string{"config.md"},
	}

	request := ResolveRequest(cfg, 7, int64(512), 2, Overrides{
		Research:     &falseValue,
		ContextSet:   true,
		ContextPaths: []string{"cli.md", "cli-dir"},
	})

	if request.ResearchEnabled {
		t.Fatal("ResearchEnabled: got true, want CLI override false")
	}
	want := []string{"cli.md", "cli-dir"}
	if len(request.ContextPaths) != len(want) || request.ContextPaths[0] != want[0] || request.ContextPaths[1] != want[1] {
		t.Fatalf("ContextPaths: got %#v, want %#v", request.ContextPaths, want)
	}
	if request.MaxSources != 7 {
		t.Fatalf("MaxSources: got %d, want settings cap 7", request.MaxSources)
	}
	if request.MaxBytes != 512 {
		t.Fatalf("MaxBytes: got %d, want settings cap 512", request.MaxBytes)
	}
	if request.MaxDepth != 2 {
		t.Fatalf("MaxDepth: got %d, want settings cap 2", request.MaxDepth)
	}
}

func TestResolveRequestUsesAutoDefaultsWhenSettingsUnset(t *testing.T) {
	cfg := &types.DeliberationConfig{ContextPaths: []string{"README.md"}}
	request := ResolveRequest(cfg, 0, int64(0), 0, Overrides{
		Defaults: DefaultsForAutoLevel(types.AutoDeep),
	})

	if request.MaxSources != 300 {
		t.Fatalf("MaxSources: got %d, want deep default 300", request.MaxSources)
	}
	if request.MaxBytes != 16<<20 {
		t.Fatalf("MaxBytes: got %d, want deep default %d", request.MaxBytes, int64(16<<20))
	}
	if request.MaxDepth != 8 {
		t.Fatalf("MaxDepth: got %d, want deep default 8", request.MaxDepth)
	}
}

func TestResolveRequestSettingsOverrideAutoDefaults(t *testing.T) {
	cfg := &types.DeliberationConfig{ContextPaths: []string{"README.md"}}
	request := ResolveRequest(cfg, 7, int64(512), 2, Overrides{
		Defaults: DefaultsForAutoLevel(types.AutoYOLO),
	})

	if request.MaxSources != 7 || request.MaxBytes != 512 || request.MaxDepth != 2 {
		t.Fatalf("evidence caps: got sources=%d bytes=%d depth=%d, want explicit settings 7/512/2", request.MaxSources, request.MaxBytes, request.MaxDepth)
	}
}

func TestDefaultsForAutoLevel(t *testing.T) {
	tests := []struct {
		level types.AutoLevel
		want  Defaults
	}{
		{types.AutoQuick, Defaults{MaxSources: 20, MaxBytes: 1 << 20, MaxDepth: 5}},
		{types.AutoNormal, Defaults{MaxSources: 40, MaxBytes: 4 << 20, MaxDepth: 6}},
		{types.AutoDeep, Defaults{MaxSources: 300, MaxBytes: 16 << 20, MaxDepth: 8}},
		{types.AutoYOLO, Defaults{MaxSources: 1000, MaxBytes: 64 << 20, MaxDepth: 12}},
		{types.AutoOff, Defaults{MaxSources: 20, MaxBytes: 1 << 20, MaxDepth: 5}},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			got := DefaultsForAutoLevel(tt.level)
			if got != tt.want {
				t.Fatalf("DefaultsForAutoLevel(%q): got %+v, want %+v", tt.level, got, tt.want)
			}
		})
	}
}

func TestResolveRequestUsesConfigResearchWithoutCLI(t *testing.T) {
	cfg := &types.DeliberationConfig{ResearchEnabled: true, ContextPaths: []string{"config.md"}}
	request := ResolveRequest(cfg, 0, int64(0), 0, Overrides{})
	if !request.ResearchEnabled {
		t.Fatal("ResearchEnabled: got false, want config-enabled research")
	}
	if len(request.ContextPaths) != 1 || request.ContextPaths[0] != "config.md" {
		t.Fatalf("ContextPaths: got %#v, want config context", request.ContextPaths)
	}
}

func TestResolveRequestSettingsDoNotEnableResearch(t *testing.T) {
	cfg := &types.DeliberationConfig{}
	request := ResolveRequest(cfg, 5, int64(0), 0, Overrides{})
	if request.ResearchEnabled {
		t.Fatal("ResearchEnabled: got true, want false because settings must not enable web access")
	}
}
