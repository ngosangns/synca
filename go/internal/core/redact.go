package core

import (
	"regexp"
	"strings"
)

// SecretMask replaces a secret value in command output. The fingerprint is
// computed from the real config before any value is replaced.
const SecretMask = "••••••"

// secretFragments are matched against an upper-case key. "-" and "_" are
// equivalent, so API_KEY also matches api-key and --api-key.
var secretFragments = []string{
	"SECRET",
	"PASSWORD",
	"PASSWD",
	"TOKEN",
	"API_KEY",
	"ACCESS_KEY",
	"PRIVATE_KEY",
}

// assignKey matches an env name or a short flag (KEY, --key, -key).
var assignKey = regexp.MustCompile(`^-{0,2}[A-Za-z_][A-Za-z0-9_-]*$`)

// inlineAssign matches KEY=value or --key=value after a boundary.
var inlineAssign = regexp.MustCompile(`(^|[\s"'])(-{0,2}[A-Za-z_][A-Za-z0-9_-]*)=([^\s"']+)`)

// inlineURL matches scheme://user:password@ and keeps the user.
var inlineURL = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^:\s/@]+):([^@\s/]+)@`)

// IsSecretKey reports whether name is a secret key under the agent CLI contract.
func IsSecretKey(name string) bool {
	upper := strings.ReplaceAll(strings.ToUpper(name), "-", "_")
	for _, fragment := range secretFragments {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func secretName(name string) bool {
	base := strings.TrimLeft(name, "-")
	if strings.EqualFold(base, "authorization") {
		return true
	}
	return IsSecretKey(base)
}

// MaskURL replaces a userinfo password with SecretMask. A string that is not
// a URL with a password is returned unchanged.
func MaskURL(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || !validScheme(scheme) {
		return raw
	}
	userinfo, tail, ok := strings.Cut(rest, "@")
	if !ok {
		return raw
	}
	user, password, ok := strings.Cut(userinfo, ":")
	if !ok || password == "" {
		return raw
	}
	return scheme + "://" + user + ":" + SecretMask + "@" + tail
}

func validScheme(scheme string) bool {
	if scheme == "" {
		return false
	}
	for i, r := range scheme {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// MaskNormalized returns a copy of n for display. Every env value is
// SecretMask. URL userinfo passwords and secret-looking command or arg
// values are masked. Env keys are unchanged. n is not modified.
func MaskNormalized(n *McpNormalized) *McpNormalized {
	if n == nil {
		return nil
	}
	out := *n
	out.Command = MaskArgv(n.Command)
	out.Args = MaskArgv(n.Args)
	if n.URL != nil {
		u := maskSecretsInString(*n.URL)
		out.URL = &u
	}
	if n.Enabled != nil {
		b := *n.Enabled
		out.Enabled = &b
	}
	if n.EnvKeys != nil {
		out.EnvKeys = append([]string{}, n.EnvKeys...)
	}
	if n.Env != nil {
		out.Env = make(map[string]string, len(n.Env))
		for k := range n.Env {
			out.Env[k] = SecretMask
		}
	}
	return &out
}

// MaskMcpEntries returns a copy of entries with normalized configs masked.
// Each fingerprint is the one already stored on the entry, computed from the
// real config. entries is not modified.
func MaskMcpEntries(entries []McpEntry) []McpEntry {
	if entries == nil {
		return nil
	}
	out := make([]McpEntry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		if entry.Presence == nil {
			continue
		}
		out[i].Presence = make([]McpPresence, len(entry.Presence))
		for j, presence := range entry.Presence {
			out[i].Presence[j] = presence
			out[i].Presence[j].Normalized = MaskNormalized(presence.Normalized)
		}
	}
	return out
}

// MaskArgv returns a copy of args with secret values masked. A nil slice
// stays nil so JSON output remains null.
func MaskArgv(args []string) []string {
	if args == nil {
		return nil
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if isSecretFlag(args[i]) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out = append(out, args[i], SecretMask)
			i++
			continue
		}
		out = append(out, maskSecretsInString(args[i]))
	}
	return out
}

func isSecretFlag(arg string) bool {
	if strings.Contains(arg, "=") || !strings.HasPrefix(arg, "-") {
		return false
	}
	name := strings.TrimLeft(arg, "-")
	if name == "" {
		return false
	}
	return secretName(name)
}

func maskSecretsInString(s string) string {
	if masked, ok := maskWholeAssignment(s); ok {
		return masked
	}
	s = maskAuthHeader(s)
	s = maskInlineAssignments(s)
	s = maskInlineURLs(s)
	return MaskURL(s)
}

func maskWholeAssignment(text string) (string, bool) {
	key, value, ok := strings.Cut(text, "=")
	if !ok || !assignKey.MatchString(key) {
		return "", false
	}
	if secretName(key) {
		return key + "=" + SecretMask, true
	}
	return key + "=" + MaskURL(value), true
}

func maskAuthHeader(s string) string {
	lower := strings.ToLower(s)
	for _, sep := range []string{"authorization:", "authorization="} {
		i := strings.Index(lower, sep)
		if i < 0 {
			continue
		}
		end := i + len(sep)
		if end < len(s) && (s[end] == ' ' || s[end] == '\t') {
			return s[:end] + " " + SecretMask
		}
		return s[:end] + SecretMask
	}
	return s
}

func maskInlineAssignments(s string) string {
	return inlineAssign.ReplaceAllStringFunc(s, func(match string) string {
		parts := inlineAssign.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		if secretName(parts[2]) {
			return parts[1] + parts[2] + "=" + SecretMask
		}
		return parts[1] + parts[2] + "=" + MaskURL(parts[3])
	})
}

func maskInlineURLs(s string) string {
	return inlineURL.ReplaceAllStringFunc(s, func(match string) string {
		parts := inlineURL.FindStringSubmatch(match)
		if len(parts) != 3 || parts[2] == "" {
			return match
		}
		return parts[1] + ":" + SecretMask + "@"
	})
}
