package updater

import "fmt"

// app is filled by the product file's init. Tests in this package see it too.
var app product

// app is the one product this binary updates. Synca and promptscope each ship
// their own copy of this package with these three fields set.
type product struct {
	name      string
	display   string
	repo      string
	companion string
}

func (p product) latestURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", p.repo)
}

func (p product) assetName(tag string) string {
	return fmt.Sprintf("%s-%s-darwin-arm64", p.name, tag)
}

// companionPrefix is the second asset installed beside the command, without a
// trailing hyphen. Empty means the release is one binary.
func (p product) companionAssetName(tag string) string {
	if p.companion == "" {
		return ""
	}
	return fmt.Sprintf("%s-%s-darwin-arm64", p.companion, tag)
}

func (p product) downloadURL(tag, name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", p.repo, tag, name)
}
