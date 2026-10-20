package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReleaseRequiresDigestAndExactURL(t *testing.T) {
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	good := releaseJSON("v1.2.3", "synca-v1.2.3-darwin-arm64", 12, digest, false, false)
	release, err := parseRelease(good)
	if err != nil {
		t.Fatal(err)
	}
	if release.version != "1.2.3" || release.sha256 != strings.TrimPrefix(digest, "sha256:") {
		t.Fatalf("parsed %+v", release)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{"draft", releaseJSON("v1.2.3", "synca-v1.2.3-darwin-arm64", 12, digest, true, false), "draft"},
		{"prerelease", releaseJSON("v1.2.3", "synca-v1.2.3-darwin-arm64", 12, digest, false, true), "prerelease"},
		{"tag", releaseJSON("1.2.3", "synca-v1.2.3-darwin-arm64", 12, digest, false, false), "not vX.Y.Z"},
		{"missing asset", releaseJSON("v1.2.3", "other", 12, digest, false, false), "no asset"},
		{"no digest", releaseJSON("v1.2.3", "synca-v1.2.3-darwin-arm64", 12, "", false, false), "digest"},
		{"bad digest", releaseJSON("v1.2.3", "synca-v1.2.3-darwin-arm64", 12, "md5:abcd", false, false), "sha256"},
		{"wrong url", strings.ReplaceAll(good, "https://github.com/ngosangns/synca", "https://evil.example"), "download url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRelease(tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestRunRefusesDowngradeAndForeignBinary(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(home, ".local", "bin", "synca")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}
	body := releaseJSON("v1.0.0", "synca-v1.0.0-darwin-arm64", 4, "sha256:"+strings.Repeat("ab", 32), false, false)
	transport := staticTransport{latest: body}

	var errOut strings.Builder
	err := Run(context.Background(), Options{
		Args:              []string{"--check"},
		Version:           "1.2.0",
		Home:              home,
		CurrentExe:        filepath.Join(home, ".local", "share", "synca", "bin", "synca-1.2.0"),
		PlatformSupported: boolPtr(true),
		Transport:         transport,
		Stderr:            &errOut,
		Stdout:            &strings.Builder{},
	})
	if err != nil {
		t.Fatal(err, errOut.String())
	}

	errOut.Reset()
	err = Run(context.Background(), Options{
		Args:              nil,
		Version:           "1.0.0",
		Home:              home,
		CurrentExe:        filepath.Join(home, "elsewhere", "synca"),
		PlatformSupported: boolPtr(true),
		Transport:         transport,
		Stderr:            &errOut,
	})
	if ExitCode(err) != 1 || !strings.Contains(errOut.String(), "only replaces") {
		t.Fatalf("exit %v stderr %q", err, errOut.String())
	}
}

func TestRunInstallsWhenDigestMatches(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(home, ".local", "share", "synca", "bin", "synca-1.0.0")
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".local", "bin", "synca")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../share/synca/bin/synca-1.0.0", link); err != nil {
		t.Fatal(err)
	}

	payload := []byte("#!/bin/sh\n")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	body := releaseJSON("v1.1.0", "synca-v1.1.0-darwin-arm64", uint64(len(payload)), "sha256:"+digest, false, false)
	transport := staticTransport{latest: body, payload: payload}

	var out strings.Builder
	err := Run(context.Background(), Options{
		Version:           "1.0.0",
		Home:              home,
		CurrentExe:        current,
		PlatformSupported: boolPtr(true),
		Transport:         transport,
		Smoke:             func(path string) (string, error) { return "1.1.0", nil },
		Stdout:            &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "updated synca to 1.1.0") {
		t.Fatalf("stdout %q", out.String())
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(target, "synca-1.1.0") {
		t.Fatalf("link target %s", target)
	}
	installed, err := os.ReadFile(filepath.Join(filepath.Dir(current), "synca-1.1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(payload) {
		t.Fatalf("installed %q", installed)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(current), "synca-1.0.0")); err != nil {
		t.Fatal("previous generation was removed", err)
	}
}

func TestRunRejectsChecksumMismatch(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(home, ".local", "share", "synca", "bin", "synca-1.0.0")
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".local", "bin", "synca")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(current, link); err != nil {
		t.Fatal(err)
	}
	body := releaseJSON("v1.1.0", "synca-v1.1.0-darwin-arm64", 4, "sha256:"+strings.Repeat("ab", 32), false, false)
	var errOut strings.Builder
	err := Run(context.Background(), Options{
		Version:           "1.0.0",
		Home:              home,
		CurrentExe:        current,
		PlatformSupported: boolPtr(true),
		Transport:         staticTransport{latest: body, payload: []byte("nope")},
		Stderr:            &errOut,
	})
	if ExitCode(err) != 1 || !strings.Contains(errOut.String(), "sha256 mismatch") {
		t.Fatalf("exit %v stderr %q", err, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(current), "synca-1.1.0")); !os.IsNotExist(err) {
		t.Fatal("mismatched download was installed")
	}
}

type staticTransport struct {
	latest  string
	payload []byte
}

func (s staticTransport) FetchLatest(context.Context) (string, error) { return s.latest, nil }

func (s staticTransport) Download(_ context.Context, _, dest string) error {
	return os.WriteFile(dest, s.payload, 0o644)
}

func releaseJSON(tag, asset string, size uint64, digest string, draft, prerelease bool) string {
	url := "https://github.com/ngosangns/synca/releases/download/" + tag + "/" + asset
	digestJSON := "null"
	if digest != "" {
		digestJSON = `"` + digest + `"`
	}
	return `{
		"tag_name": "` + tag + `",
		"draft": ` + boolText(draft) + `,
		"prerelease": ` + boolText(prerelease) + `,
		"assets": [{
			"name": "` + asset + `",
			"state": "uploaded",
			"size": ` + itoa(size) + `,
			"digest": ` + digestJSON + `,
			"browser_download_url": "` + url + `"
		}]
	}`
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func boolPtr(v bool) *bool { return &v }
