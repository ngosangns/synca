package core

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ngosangns/synca/go/internal/registry"
)

const registryTimeout = 60 * time.Second

// errNoSnapshot means skills.sh cannot serve this skill completely. Callers
// fall back to a Git clone.
var errNoSnapshot = fmt.Errorf("no complete snapshot on skills.sh")

func newRegistry() *registry.Client { return registry.New(Version) }

func registryContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), registryTimeout)
}

// skillMDFolders lists the folders in the tree that hold a SKILL.md. The root
// is "".
func skillMDFolders(t *registry.Tree) []string {
	var out []string
	for _, e := range t.Entries {
		if e.Type != "blob" {
			continue
		}
		switch {
		case e.Path == "SKILL.md":
			out = append(out, "")
		case strings.HasSuffix(e.Path, "/SKILL.md"):
			out = append(out, strings.TrimSuffix(e.Path, "/SKILL.md"))
		}
	}
	return out
}

// pickFolder finds the folder of the skill with this slug. An empty slug
// accepts the repository's only skill. The shortest matching path wins.
func pickFolder(t *registry.Tree, repo, slug string) (folder, outSlug string, ok bool) {
	folders := skillMDFolders(t)
	if slug == "" {
		if len(folders) != 1 {
			return "", "", false
		}
		f := folders[0]
		name := repo
		if f != "" {
			name = path.Base(f)
		}
		return f, SkillSlug(name), true
	}
	best, found := "", false
	for _, f := range folders {
		name := repo
		if f != "" {
			name = path.Base(f)
		}
		if SkillSlug(name) != slug {
			continue
		}
		if !found || len(f) < len(best) {
			best, found = f, true
		}
	}
	return best, slug, found
}

var snapshotSkipFiles = map[string]bool{"metadata.json": true}
var snapshotSkipDirs = map[string]bool{".git": true, "__pycache__": true, "__pypackages__": true}

func installableSnapshotPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if snapshotSkipFiles[parts[len(parts)-1]] {
		return false
	}
	for _, p := range parts[:len(parts)-1] {
		if snapshotSkipDirs[p] {
			return false
		}
	}
	return true
}

// snapshotComplete reports whether every installable file the tree lists under
// folder is in the snapshot. A root skill needs SKILL.md only.
func snapshotComplete(t *registry.Tree, folder string, files []registry.DownloadFile) bool {
	if folder == "" {
		for _, f := range files {
			if strings.EqualFold(f.Path, "SKILL.md") {
				return true
			}
		}
		return false
	}
	have := map[string]bool{}
	for _, f := range files {
		have[f.Path] = true
	}
	prefix := folder + "/"
	for _, e := range t.Entries {
		if e.Type != "blob" || !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		rel := strings.TrimPrefix(e.Path, prefix)
		if installableSnapshotPath(rel) && !have[rel] {
			return false
		}
	}
	return have["SKILL.md"]
}

func safeRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return false
	}
	for _, p := range strings.Split(rel, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}

func writeSnapshot(dir string, folder string, files []registry.DownloadFile) error {
	for _, f := range files {
		if !safeRel(f.Path) {
			return fmt.Errorf("unsafe path %q in skills.sh snapshot", f.Path)
		}
		if folder == "" && !strings.EqualFold(f.Path, "SKILL.md") {
			continue
		}
		if !installableSnapshotPath(f.Path) {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(f.Contents), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// fetchFromRegistry downloads one GitHub skill from skills.sh. tree is the
// repository tree. slug "" accepts the repository's only skill. It returns
// errNoSnapshot, or another error, when the caller should clone instead.
func fetchFromRegistry(c *registry.Client, owner, repo, slug string, tree *registry.Tree) (*Resolved, error) {
	if tree == nil || tree.Truncated {
		return nil, errNoSnapshot
	}
	folder, slug, ok := pickFolder(tree, repo, slug)
	if !ok {
		return nil, errNoSnapshot
	}
	return fetchFolder(c, owner, repo, slug, folder, tree)
}

// folderSlug is the registry slug of the skill in folder ("" is the repo root).
func folderSlug(repo, folder string) string {
	if folder == "" {
		return SkillSlug(repo)
	}
	return SkillSlug(path.Base(folder))
}

// fetchFolder downloads the skill that lives in folder.
func fetchFolder(c *registry.Client, owner, repo, slug, folder string, tree *registry.Tree) (*Resolved, error) {
	if tree == nil || tree.Truncated || slug == "" {
		return nil, errNoSnapshot
	}
	sha, ok := tree.FolderSHA(folder)
	if !ok {
		return nil, errNoSnapshot
	}
	ctx, cancel := registryContext()
	defer cancel()
	snap, err := c.Download(ctx, strings.ToLower(owner), strings.ToLower(repo), slug)
	if err != nil {
		return nil, errNoSnapshot
	}
	if !snapshotComplete(tree, folder, snap.Files) {
		return nil, errNoSnapshot
	}
	tmp, err := os.MkdirTemp("", "synca-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	dir := filepath.Join(tmp, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		cleanup()
		return nil, err
	}
	if err := writeSnapshot(dir, folder, snap.Files); err != nil {
		cleanup()
		return nil, err
	}
	skillPath := "SKILL.md"
	if folder != "" {
		skillPath = folder + "/SKILL.md"
	}
	return &Resolved{
		Dir:     dir,
		Cleanup: cleanup,
		Origin: &LockEntry{
			Source:     owner + "/" + repo,
			SourceType: "github",
			SourceURL:  "https://github.com/" + owner + "/" + repo + ".git",
			SkillPath:  skillPath,
			FolderHash: sha,
		},
	}, nil
}

// githubOwnerRepo splits a lock or spec source into owner and repo.
func githubOwnerRepo(source string) (string, string, bool) {
	parts := strings.Split(source, "/")
	if len(parts) != 2 || !segRe.MatchString(parts[0]) || !segRe.MatchString(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// fetchGitHub gets a skill through skills.sh. A nil Resolved with a nil error
// never happens: callers check the error and clone on failure.
func fetchGitHub(owner, repo, slug string) (*Resolved, error) {
	c := newRegistry()
	ctx, cancel := registryContext()
	defer cancel()
	tree, err := c.GitHubTree(ctx, owner, repo)
	if err != nil {
		return nil, errNoSnapshot
	}
	return fetchFromRegistry(c, owner, repo, slug, tree)
}

// registryOff reports SYNCA_NO_REGISTRY, which makes every install and update
// clone with git. Tests that use local repositories set it.
func registryOff() bool { return os.Getenv("SYNCA_NO_REGISTRY") != "" }
