package output

import "os"

// plainOutput checks whether the environment forces plain (no-ANSI) output.
func plainOutput() bool {
	term, hasTerm := os.LookupEnv("TERM")
	return os.Getenv("NO_COLOR") != "" || os.Getenv("CI") != "" || !hasTerm || term == "" || term == "dumb"
}

// detectRenderer selects the appropriate Renderer for the given output mode.
func detectRenderer(mode OutputMode) Renderer {
	if !plainOutput() {
		return &RichRenderer{}
	}
	return &PlainRenderer{}
}
