// Package updater installs a GitHub release asset into a versioned directory and
// points ~/.local/bin/<name> at it.
//
// The swap renames a new inode into place. Overwriting a mapped ad-hoc-signed
// binary, or running codesign --force on that inode, SIGKILLs the process on
// macOS, so a download is never re-signed and the previous file is kept for one
// generation.
//
// The release asset must carry a GitHub sha256 digest. A sidecar checksum file
// is not accepted: it can be absent, and it is published by the same release as
// the binary.
package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

const usageSuffix = "update [--check] [--json] [--force]"

// Options configures Run.
type Options struct {
	// Args are the arguments after `update`.
	Args []string
	// Version is this binary's X.Y.Z version.
	Version string
	// Stdout receives human output and JSON reports.
	Stdout io.Writer
	// Stderr receives error output.
	Stderr io.Writer
	// Force, when true, is equivalent to passing --force.
	Force bool

	// CurrentExe overrides os.Executable (tests).
	CurrentExe string
	// Home overrides the HOME environment variable (tests).
	Home string
	// PlatformSupported overrides the darwin-arm64 gate (tests).
	PlatformSupported *bool
	// Transport overrides the GitHub release client (tests).
	Transport ReleaseTransport
	// Smoke overrides the staged `--version` smoke test (tests).
	Smoke func(path string) (string, error)
}

// ExitError carries the process exit status Run would have returned: 1 for a
// failed update, 2 for a usage error.
type ExitError struct{ Code int }

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s update exited with status %d", app.name, e.Code)
}

// ExitCode maps a Run error to a process exit status.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 1
}

// Run executes `<name> update`.
func Run(ctx context.Context, opts Options) error {
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Stderr
	if errOut == nil {
		errOut = os.Stderr
	}
	currentExe := opts.CurrentExe
	if currentExe == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(errOut, "%s update: cannot resolve the current executable: %v\n", app.name, err)
			return &ExitError{Code: 1}
		}
		currentExe = exe
	}
	home := opts.Home
	if home == "" {
		home = os.Getenv("HOME")
		if home == "" {
			fmt.Fprintf(errOut, "%s update: HOME is not set\n", app.name)
			return &ExitError{Code: 1}
		}
	}
	transport := opts.Transport
	if transport == nil {
		transport = newGitHubReleaseClient(app.latestURL(), githubToken())
	}
	smoke := opts.Smoke
	if smoke == nil {
		smoke = smokeBinary
	}
	platformSupported := runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
	if opts.PlatformSupported != nil {
		platformSupported = *opts.PlatformSupported
	}
	env := updateEnv{
		args:              opts.Args,
		currentVersion:    opts.Version,
		currentExe:        currentExe,
		layout:            layoutFromHome(home),
		platformSupported: platformSupported,
	}
	code := runWith(ctx, env, transport, smoke, out, errOut, opts.Force)
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}

type updateEnv struct {
	args              []string
	currentVersion    string
	currentExe        string
	layout            layout
	platformSupported bool
}

type updateFlags struct {
	check bool
	json  bool
	force bool
}

func runWith(
	ctx context.Context,
	env updateEnv,
	transport ReleaseTransport,
	smoke func(path string) (string, error),
	out, errOut io.Writer,
	force bool,
) int {
	usage := fmt.Sprintf("usage: %s %s", app.name, usageSuffix)
	if len(env.args) == 1 && (env.args[0] == "--help" || env.args[0] == "-h") {
		fmt.Fprintln(out, usage)
		return 0
	}
	flags, err := parseUpdateArgs(env.args, usage)
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		return 2
	}
	if force {
		flags.force = true
	}
	current, ok := parseSemver(env.currentVersion)
	if !ok {
		return fail(out, errOut, flags.json, reportBase(env, nil),
			fmt.Sprintf("this binary's version %q is not X.Y.Z", env.currentVersion))
	}
	if !env.platformSupported {
		return fail(out, errOut, flags.json, reportBase(env, nil),
			"the release asset is darwin-arm64 only")
	}
	// A notarized app cannot have one nested binary replaced.
	if insideAppBundle(env.currentExe) {
		return fail(out, errOut, flags.json, reportBase(env, nil),
			fmt.Sprintf("This binary is inside a %s.app. Update the app. %s update does not replace it.", app.display, app.name))
	}
	if !installOwned(env.currentExe, env.layout) {
		return fail(out, errOut, flags.json, reportBase(env, nil),
			fmt.Sprintf("%s update only replaces %s (this process is %s)", app.name, env.layout.link, env.currentExe))
	}

	document, err := transport.FetchLatest(ctx)
	if err != nil {
		return fail(out, errOut, flags.json, reportBase(env, nil), fetchMessage(err, "GitHub request failed"))
	}
	release, err := parseRelease(document)
	if err != nil {
		return fail(out, errOut, flags.json, reportBase(env, nil), err.Error())
	}
	latest, _ := parseSemver(release.version)
	report := reportBase(env, &release)
	report.updateAvailable = semver.Compare("v"+latest, "v"+current) > 0

	if flags.check || semver.Compare("v"+latest, "v"+current) < 0 || (latest == current && !flags.force) {
		var human string
		switch {
		case semver.Compare("v"+latest, "v"+current) > 0:
			human = fmt.Sprintf("%s %s is available (current %s)", app.name, latest, current)
		case latest == current:
			human = fmt.Sprintf("%s %s is already up to date", app.name, current)
		default:
			human = fmt.Sprintf("%s %s is newer than the latest release %s", app.name, current, latest)
		}
		return succeed(out, errOut, flags.json, report, human)
	}

	lock, err := acquireUpdateLock(env.layout.dir)
	if err != nil {
		return fail(out, errOut, flags.json, report, err.Error())
	}
	defer lock.release()
	inode, err := fileID(env.layout.link)
	if err != nil {
		return fail(out, errOut, flags.json, report,
			fmt.Sprintf("cannot stat %s: %v", env.layout.link, err))
	}

	partial := filepath.Join(env.layout.dir, fmt.Sprintf(".%s-%s.%d.partial", app.name, release.version, os.Getpid()))
	guard := &partialFile{path: partial}
	defer guard.remove()
	if message := stageVerified(ctx, transport, smoke, release.url, partial, release.size, release.sha256, release.version); message != "" {
		return fail(out, errOut, flags.json, report, message)
	}
	var companionPartial string
	if release.companion != nil {
		companionPartial = filepath.Join(env.layout.dir, fmt.Sprintf(".%s-%s.%d.partial", app.companion, release.version, os.Getpid()))
		companionGuard := &partialFile{path: companionPartial}
		defer companionGuard.remove()
		// Smoke the companion too. Promptscope's next `update` is this binary, so a
		// bad one must not be installed after the CLI symlink already moved.
		if message := stageVerified(ctx, transport, smoke, release.companion.url, companionPartial, release.companion.size, release.companion.sha256, release.version); message != "" {
			return fail(out, errOut, flags.json, report, message)
		}
		companionPath := env.layout.companion(release.version)
		if _, err := rotateIntoPlace(companionPartial, companionPath); err != nil {
			return fail(out, errOut, flags.json, report, err.Error())
		}
	}

	finalPath := env.layout.versioned(release.version)
	// Renamed into place, never written over the live inode and never re-signed.
	previousFile, err := rotateIntoPlace(partial, finalPath)
	if err != nil {
		return fail(out, errOut, flags.json, report, err.Error())
	}
	var previousVersion *string
	if target, err := os.Readlink(env.layout.link); err == nil {
		if version, ok := versionFromName(target); ok {
			previousVersion = &version
		}
	}
	sameVersion := previousVersion != nil && *previousVersion == release.version
	if err := ensureLinkUnchanged(env.layout.link, inode); err != nil {
		undoRotation(partial, finalPath, previousFile, sameVersion)
		return fail(out, errOut, flags.json, report, err.Error())
	}
	if _, err := publishLink(env.layout, finalPath); err != nil {
		undoRotation(partial, finalPath, previousFile, sameVersion)
		return fail(out, errOut, flags.json, report, err.Error())
	}

	keep := []string{release.version}
	if previousVersion != nil {
		keep = append(keep, *previousVersion)
	}
	pruneOldVersions(env.layout.dir, keep)

	report.updateAvailable = false
	report.installPath = finalPath
	var human string
	if semver.Compare("v"+latest, "v"+current) > 0 {
		human = fmt.Sprintf("updated %s to %s", app.name, latest)
	} else {
		human = fmt.Sprintf("reinstalled %s %s", app.name, latest)
	}
	return succeed(out, errOut, flags.json, report, human)
}

// stageVerified downloads url to dest, checks size and sha256, and marks it
// executable. smoke runs only when expectVersion is set.
func stageVerified(ctx context.Context, transport ReleaseTransport, smoke func(path string) (string, error), url, dest string, size uint64, sha, expectVersion string) string {
	if err := transport.Download(ctx, url, dest); err != nil {
		return fetchMessage(err, "download failed")
	}
	if err := verifyFile(dest, size, sha); err != nil {
		return err.Error()
	}
	if err := ensureExecutable(dest); err != nil {
		return err.Error()
	}
	if expectVersion == "" {
		return ""
	}
	printed, err := smoke(dest)
	if err != nil {
		return err.Error()
	}
	if printed != expectVersion {
		return fmt.Sprintf("smoke test printed %q, expected %s %s", app.name+" "+printed, app.name, expectVersion)
	}
	return ""
}

func parseUpdateArgs(args []string, usage string) (updateFlags, error) {
	var flags updateFlags
	for _, arg := range args {
		switch {
		case arg == "--check" && !flags.check:
			flags.check = true
		case arg == "--json" && !flags.json:
			flags.json = true
		case arg == "--force" && !flags.force:
			flags.force = true
		case arg == "--check" || arg == "--json" || arg == "--force":
			return flags, fmt.Errorf("duplicate flag: %s", arg)
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			return flags, errors.New(usage)
		}
	}
	return flags, nil
}

type report struct {
	current         string
	latest          *string
	updateAvailable bool
	asset           *string
	installPath     string
	err             *string
}

func reportBase(env updateEnv, release *Release) report {
	r := report{
		current:     env.currentVersion,
		installPath: env.layout.link,
	}
	if release != nil {
		latest := release.version
		asset := release.asset
		r.latest = &latest
		r.asset = &asset
		r.installPath = env.layout.versioned(release.version)
	}
	return r
}

func succeed(out, errOut io.Writer, jsonMode bool, r report, human string) int {
	emit(out, errOut, jsonMode, r, human, true)
	return 0
}

func insideAppBundle(path string) bool {
	return strings.Contains(path, ".app/Contents/")
}

func fail(out, errOut io.Writer, jsonMode bool, r report, message string) int {
	r.err = &message
	emit(out, errOut, jsonMode, r, message, false)
	return 1
}

func emit(out, errOut io.Writer, jsonMode bool, r report, human string, ok bool) {
	if jsonMode {
		fmt.Fprintln(out, jsonReport(r))
		return
	}
	if ok {
		for _, line := range strings.Split(human, "\n") {
			fmt.Fprintln(out, line)
		}
	} else {
		fmt.Fprintf(errOut, "%s update: %s\n", app.name, human)
	}
}

func jsonReport(r report) string {
	type pair struct {
		key   string
		value any
	}
	pairs := []pair{
		{"current", r.current},
		{"latest", r.latest},
		{"updateAvailable", r.updateAvailable},
		{"asset", r.asset},
		{"installPath", r.installPath},
	}
	if r.err != nil {
		pairs = append(pairs, pair{"error", *r.err})
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(marshalJSON(p.key))
		b.WriteByte(':')
		b.WriteString(marshalJSON(p.value))
	}
	b.WriteByte('}')
	return b.String()
}
