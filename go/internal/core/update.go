package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	GithubRepo = "ngosangns/synca"
	Version    = "0.1.12"
)

func ReleaseAPIURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GithubRepo)
}

func InstallBinDir() string {
	return filepath.Join(HomeDir(), ".local/share/synca/bin")
}

func SymlinkPath() string {
	return filepath.Join(HomeDir(), ".local/bin/synca")
}

func ExpectedAssetName(version string) string {
	ver := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("synca-v%s-darwin-arm64", ver)
}

type UpdateInfo struct {
	Current         string  `json:"current"`
	Latest          *string `json:"latest"`
	UpdateAvailable bool    `json:"update_available"`
	ReleaseURL      *string `json:"release_url"`
	AssetName       *string `json:"asset_name"`
	AssetURL        *string `json:"asset_url"`
	Sha256URL       *string `json:"sha256_url"`
	Message         string  `json:"message"`
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("synca/%s", Version))
	return httpClient().Do(req)
}

func CheckUpdate() (*UpdateInfo, error) {
	resp, err := get(ReleaseAPIURL())
	if err != nil {
		return &UpdateInfo{
			Current: Version,
			Message: fmt.Sprintf("network error: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return &UpdateInfo{
			Current: Version,
			Message: fmt.Sprintf("no releases yet for %s", GithubRepo),
		}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(body))
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	assetName := ExpectedAssetName(latest)

	var asset, sha *struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}
	for i := range release.Assets {
		a := &release.Assets[i]
		if a.Name == assetName || a.Name == assetName+".tar.gz" {
			asset = a
		}
		if a.Name == assetName+".sha256" || a.Name == assetName+".sha256sum" {
			sha = a
		}
	}

	updateAvailable := latest != Version
	var message string
	if updateAvailable {
		message = fmt.Sprintf("update available: %s → %s", Version, latest)
	} else {
		message = fmt.Sprintf("already up to date (%s)", Version)
	}

	info := &UpdateInfo{
		Current:         Version,
		Latest:          &latest,
		UpdateAvailable: updateAvailable,
		Message:         message,
	}
	if release.HTMLURL != "" {
		info.ReleaseURL = &release.HTMLURL
	}
	if asset != nil {
		info.AssetName = &asset.Name
		info.AssetURL = &asset.BrowserDownloadURL
	}
	if sha != nil {
		info.Sha256URL = &sha.BrowserDownloadURL
	}
	return info, nil
}

func InstallUpdate(force bool) (*UpdateInfo, error) {
	info, err := CheckUpdate()
	if err != nil {
		return nil, err
	}
	if !info.UpdateAvailable && !force {
		return info, nil
	}
	if info.Latest == nil {
		return nil, fmt.Errorf("no latest version from GitHub")
	}
	latest := *info.Latest
	if info.AssetURL == nil {
		return nil, fmt.Errorf("no asset named %s in release (publish darwin-arm64 binary)", ExpectedAssetName(latest))
	}
	assetName := ExpectedAssetName(latest)
	if info.AssetName != nil {
		assetName = *info.AssetName
	}

	resp, err := get(*info.AssetURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if info.Sha256URL != nil {
		if sresp, err := get(*info.Sha256URL); err == nil && sresp.StatusCode == 200 {
			body, _ := io.ReadAll(io.LimitReader(sresp.Body, 4096))
			sresp.Body.Close()
			fields := strings.Fields(string(body))
			if len(fields) > 0 {
				expected := strings.ToLower(strings.TrimSpace(fields[0]))
				sum := sha256.Sum256(data)
				actual := hex.EncodeToString(sum[:])
				if expected != "" && actual != expected {
					return nil, fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actual)
				}
			}
		} else if sresp != nil {
			sresp.Body.Close()
		}
	}

	if strings.HasSuffix(assetName, ".tar.gz") {
		return nil, fmt.Errorf("tar.gz assets not supported; publish raw binary named %s", ExpectedAssetName(latest))
	}

	binDir := InstallBinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, err
	}
	versioned := filepath.Join(binDir, fmt.Sprintf("synca-%s", latest))
	if err := os.WriteFile(versioned, data, 0o755); err != nil {
		return nil, err
	}

	link := SymlinkPath()
	if parent := filepath.Dir(link); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, err
		}
	}
	if _, err := os.Lstat(link); err == nil {
		_ = os.Remove(link)
	}
	if err := os.Symlink(versioned, link); err != nil {
		return nil, err
	}

	info.Message = fmt.Sprintf("installed %s -> %s", link, versioned)
	info.UpdateAvailable = false
	return info, nil
}
