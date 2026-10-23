package core

import "testing"

func TestMaskNormalizedHidesEveryEnvValue(t *testing.T) {
	url := "https://bot:fake-token-123@example.com/mcp"
	on := true
	real := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"echo --token=fake-token-123"},
		URL:       &url,
		Args: []string{
			"--token=fake-token-123",
			"--api-key",
			"fake-token-123",
			"Authorization: Bearer fake-token-123",
			"-y",
		},
		Enabled: &on,
		EnvKeys: []string{"DISCORD_TOKEN", "LOG_LEVEL"},
		Env: map[string]string{
			"DISCORD_TOKEN": "fake-token-123",
			"LOG_LEVEL":     "info",
		},
	}
	fp := McpFingerprint(real)
	masked := MaskNormalized(real)

	if real.Env["DISCORD_TOKEN"] == SecretMask || real.Env["LOG_LEVEL"] != "info" {
		t.Fatal("masking mutated the real env map")
	}
	if real.Args[0] == "--token="+SecretMask {
		t.Fatal("masking mutated the real args")
	}
	if McpFingerprint(real) != fp {
		t.Fatal("masking changed the fingerprint inputs")
	}
	if masked.Env["DISCORD_TOKEN"] != SecretMask || masked.Env["LOG_LEVEL"] != SecretMask {
		t.Fatal("env values were not masked")
	}
	if len(masked.EnvKeys) != 2 || masked.EnvKeys[0] != "DISCORD_TOKEN" || masked.EnvKeys[1] != "LOG_LEVEL" {
		t.Fatal("env keys changed")
	}
	if masked.URL == nil || *masked.URL != "https://bot:"+SecretMask+"@example.com/mcp" {
		t.Fatal("url password was not masked")
	}
	if masked.Command[0] != "echo --token="+SecretMask {
		t.Fatal("command assignment was not masked")
	}
	if masked.Args[0] != "--token="+SecretMask || masked.Args[1] != "--api-key" || masked.Args[2] != SecretMask {
		t.Fatal("args were not masked")
	}
	if masked.Args[3] != "Authorization: "+SecretMask || masked.Args[4] != "-y" {
		t.Fatal("header or plain arg was wrong")
	}
	if McpFingerprint(masked) == fp {
		t.Fatal("masked url hashed like the real url; display must keep the stored fingerprint")
	}
	if masked.Enabled == nil || !*masked.Enabled || masked.Enabled == real.Enabled {
		t.Fatal("enabled pointer was shared or dropped")
	}
}

func TestMaskMcpEntriesKeepsStoredFingerprint(t *testing.T) {
	url := "https://bot:fake-token-123@example.com/mcp"
	real := &McpNormalized{
		Transport: "http",
		URL:       &url,
		EnvKeys:   []string{"TOKEN"},
		Env:       map[string]string{"TOKEN": "fake-token-123"},
	}
	fp := McpFingerprint(real)
	entries := []McpEntry{{
		Key: "remote",
		Presence: []McpPresence{{
			Agent:       "agents",
			Fingerprint: fp,
			Normalized:  real,
		}},
	}}
	out := MaskMcpEntries(entries)
	if out[0].Presence[0].Fingerprint != fp {
		t.Fatal("stored fingerprint changed")
	}
	if entries[0].Presence[0].Normalized.Env["TOKEN"] == SecretMask {
		t.Fatal("entry env was mutated")
	}
	if out[0].Presence[0].Normalized.Env["TOKEN"] != SecretMask {
		t.Fatal("displayed env was not masked")
	}
}

func TestMaskURLKeepsUser(t *testing.T) {
	if got := MaskURL("https://example.com/mcp"); got != "https://example.com/mcp" {
		t.Fatalf("plain url %q", got)
	}
	if got := MaskURL("https://bot@example.com/mcp"); got != "https://bot@example.com/mcp" {
		t.Fatalf("user only %q", got)
	}
	if got := MaskURL("not a url"); got != "not a url" {
		t.Fatalf("text %q", got)
	}
}

func TestMaskArgvNilStaysNil(t *testing.T) {
	if MaskArgv(nil) != nil {
		t.Fatal("nil args became a slice")
	}
	if got := MaskArgv([]string{}); len(got) != 0 {
		t.Fatalf("empty %v", got)
	}
}
