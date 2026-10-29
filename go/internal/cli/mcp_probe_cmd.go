package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ngosangns/synca/go/internal/core"
)

// runMcpProbe connects to MCP servers and estimates their context cost from
// the live tool definitions. Results are cached for `mcp list` and the app;
// per-server failures are rows in the output, not command failures.
func runMcpProbe(f *flags, jsonOut bool) error {
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp probe --help")
	}
	timeout := core.DefaultProbeTimeout
	if raw := f.str("timeout", ""); raw != "" {
		seconds, parseErr := strconv.Atoi(raw)
		if parseErr != nil || seconds < 1 || seconds > 120 {
			return failUsage(jsonOut, "invalid --timeout "+quoteArg(raw)+" (use seconds between 1 and 120)", "see: synca mcp probe --help")
		}
		timeout = time.Duration(seconds) * time.Second
	}

	started := time.Now()
	outcomes, err := core.ProbeMcps(scope, cwdOf(f), f.pos, timeout)
	if err != nil {
		// The measurements still stand; only the cache write failed.
		fmt.Fprintln(os.Stderr, "probe:", err)
	}
	elapsed := time.Since(started).Round(100 * time.Millisecond)

	if jsonOut {
		return writeJSON(outcomes)
	}
	fmt.Printf("MCP probe (%s)\n", scope)
	probed := 0
	for _, outcome := range outcomes {
		state, detail := "ok", ""
		switch {
		case outcome.Skipped:
			state = "skip"
		case !outcome.OK:
			state = "fail"
		default:
			probed++
			detail = fmt.Sprintf("%s · %s tokens", probeCounts(outcome), fmtTokens(outcome.Tokens))
		}
		name := outcome.Key
		if outcome.Server != "" {
			name = outcome.Key + " (" + outcome.Server + ")"
		}
		fields := []string{name}
		if outcome.Transport != "" {
			fields = append(fields, outcome.Transport)
		}
		if detail != "" {
			fields = append(fields, detail)
		}
		fmt.Printf("  %-4s  %s\n", state, strings.Join(fields, "  "))
		if state != "ok" && outcome.Error != "" {
			fmt.Printf("           %s\n", outcome.Error)
		}
	}
	fmt.Printf("%d/%d probed in %s — results cached for mcp list\n", probed, len(outcomes), elapsed)
	return nil
}

// probeCounts is the "3 tools, 2 prompts" summary; empty when the server
// lists nothing.
func probeCounts(outcome core.McpProbeOutcome) string {
	parts := []string{}
	if outcome.Tools > 0 {
		parts = append(parts, fmt.Sprintf("%d tool%s", outcome.Tools, plural(outcome.Tools)))
	}
	if outcome.Resources > 0 {
		parts = append(parts, fmt.Sprintf("%d resource%s", outcome.Resources, plural(outcome.Resources)))
	}
	if outcome.Prompts > 0 {
		parts = append(parts, fmt.Sprintf("%d prompt%s", outcome.Prompts, plural(outcome.Prompts)))
	}
	return strings.Join(parts, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// fmtTokens matches the app's ≈820 / ≈1.2k / ≈2.4M shape.
func fmtTokens(tokens int) string {
	value, unit := float64(tokens), ""
	switch {
	case tokens >= 1_000_000:
		value, unit = value/1_000_000, "M"
	case tokens >= 1000:
		value, unit = value/1000, "k"
	default:
		return fmt.Sprintf("≈%d", tokens)
	}
	text := strconv.FormatFloat(value, 'f', 1, 64)
	text = strings.TrimSuffix(text, ".0")
	return fmt.Sprintf("≈%s%s", text, unit)
}
