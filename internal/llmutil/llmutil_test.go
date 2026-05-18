package llmutil

import (
	"reflect"
	"testing"
)

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "yaml fence",
			input: "```yaml\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
		{
			name:  "yml fence",
			input: "```yml\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
		{
			name:  "json fence",
			input: "```json\n{\"key\": \"value\"}\n```\n",
			want:  `{"key": "value"}`,
		},
		{
			name:  "bare fence",
			input: "```\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
		{
			name:  "no fence",
			input: "topology: ring\n",
			want:  "topology: ring",
		},
		{
			name:  "fence with preamble",
			input: "Here is the config:\n```yaml\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripCodeFences(tt.input)
			if got != tt.want {
				t.Errorf("StripCodeFences(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]any
		wantErr bool
	}{
		{
			name:  "raw json",
			input: `{"key_arguments": ["arg1"]}`,
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "json code block",
			input: "```json\n{\"key_arguments\": [\"arg1\"]}\n```",
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "plain code block",
			input: "```\n{\"key_arguments\": [\"arg1\"]}\n```",
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "embedded json with trailing text",
			input: `Here are the queries: {"queries":["q1", "q2"]}` + "\n\nqueries complete",
			want:  map[string]any{"queries": []any{"q1", "q2"}},
		},
		{
			name:  "multiple blocks picks first",
			input: "```json\n{\"first\": true}\n```\n```json\n{\"second\": true}\n```",
			want:  map[string]any{"first": true},
		},
		{
			name:  "json in code block with text",
			input: "Here is the result:\n```json\n{\"confidence\": \"high\"}\n```\nDone.",
			want:  map[string]any{"confidence": "high"},
		},
		{
			name:    "no json found",
			input:   "just text without json",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "malformed json",
			input:   `{"key_arguments": broken}`,
			wantErr: true,
		},
		{
			name:    "backtick after array element",
			input:   `{"queries": ["query1"` + "`" + `"query2"]}`,
			wantErr: true,
		},
		{
			name:  "backtick stripped from inline code",
			input: "Here is the result:\n`{\"queries\": [\"q1\", \"q2\"]}`",
			want:  map[string]any{"queries": []any{"q1", "q2"}},
		},
		{
			name:  "stray backtick between array elements",
			input: `{"queries": ["query1", "query2"]}` + "`" + ` {"more": true}`,
			want:  map[string]any{"queries": []any{"query1", "query2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			err := ExtractJSON(tt.input, &got)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
