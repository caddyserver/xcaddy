// Copyright 2020 Matthew Holt
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xcaddy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/caddyserver/xcaddy/internal/utils"
)

// Builder can produce a custom Caddy build with the
// configuration it represents.
type Builder struct {
	Compile
	CaddyVersion string        `json:"caddy_version,omitempty"`
	Plugins      []Dependency  `json:"plugins,omitempty"`
	Replacements []Replace     `json:"replacements,omitempty"`
	TimeoutGet   time.Duration `json:"timeout_get,omitempty"`
	TimeoutBuild time.Duration `json:"timeout_build,omitempty"`
	RaceDetector bool          `json:"race_detector,omitempty"`
	SkipCleanup  bool          `json:"skip_cleanup,omitempty"`
	SkipBuild    bool          `json:"skip_build,omitempty"`
	Debug        bool          `json:"debug,omitempty"`
	BuildFlags   string        `json:"build_flags,omitempty"`
	ModFlags     string        `json:"mod_flags,omitempty"`
	PgoProfile   string        `json:"pgo_profile,omitempty"` // Experimental

	// Experimental: subject to change
	EmbedDirs []struct {
		Dir  string `json:"dir,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"embed_dir,omitempty"`

	// OnStep, if set, is called for each build step, making the
	// build's progress observable and controllable. The callback
	// runs in its own goroutine, concurrently with the step it
	// describes, so it can consume the event's Output directly,
	// with no extra goroutine of its own:
	//
	//	OnStep: func(e *xcaddy.StepEvent) error {
	//		scanner := bufio.NewScanner(e.Output)
	//		for scanner.Scan() {
	//			showProgress(e.Step, scanner.Text())
	//		}
	//		return scanner.Err()
	//	}
	//
	// Callbacks never overlap: the next step does not begin until
	// the previous step's callback has returned, and Build does
	// not return until the final callback has returned. Output
	// reaches EOF when the step ends, so a callback that reads
	// until EOF returns naturally at the step boundary; one that
	// lingers afterward delays the build accordingly.
	//
	// Returning a non-nil error aborts the build at the end of
	// the step: no further steps begin, and the error is returned
	// from Build, wrapped with the step name (use errors.Is or
	// errors.As to match a custom error). To cancel mid-step,
	// cancel the context passed to Build instead. Aborting does
	// not skip removal of the temporary build folder. The
	// callback must not call back into the Builder.
	OnStep func(event *StepEvent) error `json:"-"`

	// Env, if non-nil, is used as the entire environment of the
	// underlying go commands, in KEY=value form (see os.Environ);
	// the process's own environment is not inherited. This
	// enables hermetic, credential-free builds: if build logs are
	// published (see OnStep), no ambient credential (GOPROXY
	// userinfo, git configuration, .netrc, cloud keys, ...) can
	// leak into them when the build never had access to any. The
	// environment must include everything the go toolchain needs,
	// at minimum PATH and HOME (or GOCACHE and GOMODCACHE); GOOS,
	// GOARCH, GOARM and CGO_ENABLED are set by the Builder
	// itself. If nil, the process environment is inherited, which
	// is the historical behavior.
	Env []string `json:"-"`

	// Secrets lists sensitive values (tokens, passwords, keys)
	// that must not appear in step output: every occurrence is
	// replaced with "[REDACTED]" before it reaches a StepEvent's
	// Output. Matching is exact and line-buffered, so a secret
	// cannot slip through by straddling a write boundary; values
	// containing newlines cannot be matched. Secrets has no
	// effect when OnStep is nil.
	Secrets []string `json:"-"`

	// RedactCredentials additionally masks common credential
	// shapes in step output: userinfo in URLs, well-known token
	// formats (GitHub, GitLab, Slack, AWS access key IDs), and
	// PEM-encoded private key blocks. It is a best-effort
	// backstop for publishable logs, not a guarantee; prefer
	// building in a credential-free environment (see Env). It has
	// no effect when OnStep is nil.
	RedactCredentials bool `json:"-"`
}

// StepEvent describes a build step that is about to begin. It is
// passed to a Builder's OnStep callback.
type StepEvent struct {
	// Step identifies the step that is beginning.
	Step Step

	// Output reads everything the step produces: xcaddy's own log
	// lines (in their usual "[INFO] ..." format) as well as the
	// stdout and stderr of the underlying go commands (go mod,
	// go get, go build, ...). The Builder buffers this output
	// internally, so the build never blocks on a slow reader
	// mid-step; output is available to read the moment it is
	// produced.
	//
	// When the step ends, the Builder closes the write side, so
	// after any remaining buffered output is drained the reader
	// reaches EOF; reading until EOF is the only lifecycle a
	// consumer needs. Output that is never read is discarded when
	// the step's buffer is released.
	Output io.Reader
}

// Step identifies a phase of the build process. Steps are
// reported to a Builder's OnStep callback, if set.
type Step string

// The build steps, in the order they occur. StepWindowsResources
// only occurs when targeting Windows; StepTidyModule and
// StepCompile do not occur if SkipBuild is enabled; and
// StepCleanup does not occur if SkipCleanup is enabled.
const (
	StepCreateEnvironment Step = "create_environment" // set up the temporary build module
	StepInitializeModule  Step = "initialize_module"  // go mod init and module replacements
	StepPinVersions       Step = "pin_versions"       // go get Caddy and plugins
	StepWindowsResources  Step = "windows_resources"  // generate Windows version resources
	StepTidyModule        Step = "tidy_module"        // go mod tidy
	StepCompile           Step = "compile"            // go build
	StepCleanup           Step = "cleanup"            // remove the temporary folder
)

// buildOutput routes all output of a single build (xcaddy's own
// log lines and the go commands' stdout/stderr) to the writer
// designated for the current step by the OnStep callback.
type buildOutput struct {
	onStep func(*StepEvent) error
	logger *log.Logger

	// redaction configuration applied to each step's output
	rules         []redactRule
	maskKeyBlocks bool

	mu   sync.Mutex
	sink io.WriteCloser // write side for the current step; nil = defaults

	// the currently-running step callback; accessed only from the
	// build's own goroutine (in beginStep/endStep), never concurrently
	handlerStep Step
	handlerDone chan error
}

// configureRedaction compiles the Builder's redaction settings
// into rules applied to every step's output.
func (o *buildOutput) configureRedaction(secrets []string, redactCredentials bool) {
	for _, s := range secrets {
		if s == "" {
			continue
		}
		o.rules = append(o.rules, redactRule{literal: []byte(s)})
	}
	if redactCredentials {
		o.rules = append(o.rules, builtinCredentialRules...)
		o.maskKeyBlocks = true
	}
}

func newBuildOutput(onStep func(*StepEvent) error) *buildOutput {
	out := &buildOutput{onStep: onStep}
	if onStep == nil {
		// historical behavior: the standard log package's default logger
		out.logger = log.Default()
	} else {
		out.logger = log.New(out.stderr(), "", log.LstdFlags)
	}
	return out
}

// beginStep marks the start of a build step: it first ends the
// previous step, if any -- waiting for its callback to return --
// and then starts the OnStep callback for the new step in its own
// goroutine, handing it a reader over the step's output. A
// non-nil error means the previous step's callback aborted the
// build, and the new step must not run.
func (o *buildOutput) beginStep(step Step) error {
	if o.onStep == nil {
		return nil
	}
	if err := o.endStep(); err != nil {
		return err
	}
	pipe := newBufferedPipe()
	var sink io.WriteCloser = pipe
	if len(o.rules) > 0 || o.maskKeyBlocks {
		sink = &redactor{dst: pipe, rules: o.rules, maskKeyBlocks: o.maskKeyBlocks}
	}
	event := &StepEvent{Step: step, Output: pipe}
	done := make(chan error, 1)
	go func() {
		done <- o.onStep(event)
	}()
	o.mu.Lock()
	o.sink = sink
	o.mu.Unlock()
	o.handlerStep = step
	o.handlerDone = done
	return nil
}

// endStep ends the current step, if any: it closes the write side
// of the step's buffered pipe -- once the remaining buffered
// output is drained, the callback's reader reaches EOF, signaling
// that the step's output is complete -- and then waits for the
// step's callback to return. By the time endStep is called, all
// of the step's commands have finished, so all output has been
// produced and EOF is guaranteed to arrive; a callback that reads
// until EOF can therefore never deadlock the build. A non-nil
// error from the callback aborts the build.
func (o *buildOutput) endStep() error {
	o.mu.Lock()
	sink := o.sink
	o.sink = nil
	o.mu.Unlock()
	if sink == nil {
		return nil
	}
	sink.Close()
	err := <-o.handlerDone
	o.handlerDone = nil
	if err != nil {
		return fmt.Errorf("step %s: %w", o.handlerStep, err)
	}
	return nil
}

// stdout and stderr return writers to wire subprocess output to;
// they write to the current step's writer, or fall back to the
// conventional os stream if none is designated.
func (o *buildOutput) stdout() io.Writer { return outputFacet{o, os.Stdout} }
func (o *buildOutput) stderr() io.Writer { return outputFacet{o, os.Stderr} }

type outputFacet struct {
	out      *buildOutput
	fallback io.Writer
}

// Write routes p to the current step's sink (the buffered pipe,
// possibly behind a redactor), falling back to the facet's default
// stream if no step is active.
func (f outputFacet) Write(p []byte) (int, error) {
	f.out.mu.Lock()
	sink := f.out.sink
	f.out.mu.Unlock()
	if sink == nil {
		return f.fallback.Write(p)
	}
	return sink.Write(p)
}

// bufferedPipe is an in-memory pipe with a write side that never
// blocks: writes append to an internal buffer, and reads block
// until data is available or the write side has been closed, at
// which point draining the remaining buffer ends in io.EOF.
// It is safe for concurrent use; os/exec drains a command's
// stdout and stderr on separate goroutines, and the serialization
// here means step output is delivered to the consumer's reader
// atomically per write.
type bufferedPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newBufferedPipe() *bufferedPipe {
	p := new(bufferedPipe)
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *bufferedPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		// the step already ended; discard rather than error so a
		// stray late write can never fail the build
		return len(b), nil
	}
	n, err := p.buf.Write(b)
	p.cond.Broadcast()
	return n, err
}

func (p *bufferedPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.buf.Len() == 0 && !p.closed {
		p.cond.Wait()
	}
	if p.buf.Len() == 0 {
		return 0, io.EOF
	}
	return p.buf.Read(b)
}

// Close closes the write side of the pipe: subsequent writes are
// discarded, and readers reach EOF once the buffer is drained.
func (p *bufferedPipe) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.cond.Broadcast()
	return nil
}

// Build builds Caddy at the configured version with the
// configured plugins and plops down a binary at outputFile.
func (b Builder) Build(ctx context.Context, outputFile string) (err error) {
	out := newBuildOutput(b.OnStep)
	out.configureRedaction(b.Secrets, b.RedactCredentials)
	// registered before buildEnv.Close (deferred below), so it runs
	// after it: it ends the final step -- normally cleanup --
	// delivering its EOF and waiting for its callback to return, so
	// that no callback goroutine outlives Build
	defer func() {
		err = errors.Join(err, out.endStep())
	}()
	if err := out.beginStep(StepCreateEnvironment); err != nil {
		return err
	}
	logger := out.logger

	var cancel context.CancelFunc
	if b.TimeoutBuild > 0 {
		ctx, cancel = context.WithTimeout(ctx, b.TimeoutBuild)
		defer cancel()
	}
	if outputFile == "" {
		return fmt.Errorf("output file path is required")
	}
	// the user's specified output file might be relative, and
	// because the `go build` command is executed in a different,
	// temporary folder, we convert the user's input to an
	// absolute path so it goes the expected place
	absOutputFile, err := filepath.Abs(outputFile)
	if err != nil {
		return err
	}
	logger.Printf("[INFO] absolute output file path: %s", absOutputFile)

	// likewise with the PGO profile
	absPgoProfile, err := filepath.Abs(b.PgoProfile)
	if err != nil {
		return err
	}

	// set some defaults from the environment, if applicable
	if b.OS == "" {
		b.OS = utils.GetGOOS()
	}
	if b.Arch == "" {
		b.Arch = utils.GetGOARCH()
	}
	if b.ARM == "" {
		b.ARM = os.Getenv("GOARM")
	}

	// prepare the build environment
	buildEnv, err := b.newEnvironment(ctx, out)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, buildEnv.Close())
	}()

	// generating windows resources for embedding
	if b.OS == "windows" {
		if err := out.beginStep(StepWindowsResources); err != nil {
			return err
		}

		// get version string, we need to parse the output to get the exact version instead tag, branch or commit
		cmd, err := buildEnv.newGoBuildCommand(ctx, "list", "-m", buildEnv.caddyModulePath)
		if err != nil {
			return err
		}
		var buffer bytes.Buffer
		cmd.Stdout = &buffer
		err = buildEnv.runCommand(ctx, cmd)
		if err != nil {
			return err
		}

		// output looks like: github.com/caddyserver/caddy/v2 v2.7.6
		version := strings.TrimPrefix(buffer.String(), buildEnv.caddyModulePath)
		// if caddy replacement is a local directory, version will be
		// like v2.8.4 => c:\Users\test\caddy
		// see https://github.com/caddyserver/xcaddy/issues/215
		// strings.Cut return the string unchanged if separator is not found
		version, _, _ = strings.Cut(version, "=>")
		version = strings.TrimSpace(version)
		err = utils.WindowsResource(version, outputFile, buildEnv.tempFolder)
		if err != nil {
			return err
		}
	}

	if b.SkipBuild {
		logger.Printf("[INFO] Skipping build as requested")

		return nil
	}

	// prepare the environment for the go command: either the
	// caller's hermetic environment, or (for the most part) an
	// inheritance of our current environment, with a few
	// customizations
	env := b.Env
	if env == nil {
		env = os.Environ()
	} else {
		env = append([]string(nil), env...) // don't mutate the caller's slice
	}
	env = setEnv(env, "GOOS="+b.OS)
	env = setEnv(env, "GOARCH="+b.Arch)
	env = setEnv(env, "GOARM="+b.ARM)
	if b.RaceDetector && !b.Compile.Cgo {
		logger.Println("[WARNING] Enabling cgo because it is required by the race detector")
		b.Compile.Cgo = true
	}
	env = setEnv(env, fmt.Sprintf("CGO_ENABLED=%s", b.Compile.CgoEnabled()))

	logger.Println("[INFO] Building Caddy")

	// tidy the module to ensure go.mod and go.sum are consistent with the module prereq
	if err := out.beginStep(StepTidyModule); err != nil {
		return err
	}
	tidyCmd := buildEnv.newGoModCommand(ctx, "tidy", "-e")
	if err := buildEnv.runCommand(ctx, tidyCmd); err != nil {
		return err
	}

	// Turn the temp module into a git repo with a single commit so the Go
	// toolchain stamps VCS build info (vcs.revision/vcs.time) into the
	// binary. Some plugins read this via debug.ReadBuildInfo(); without it
	// e.g. Tailscale reports its version as "x.y.z-ERR-BuildInfo". Done
	// after tidy so go.mod/go.sum are committed (vcs.modified=false), and
	// best-effort so a missing git never fails an otherwise-valid build.
	buildEnv.initVCS(ctx)

	// compile
	if err := out.beginStep(StepCompile); err != nil {
		return err
	}
	cmd, err := buildEnv.newGoBuildCommand(ctx, "build",
		"-o", absOutputFile,
	)
	if b.PgoProfile != "" {
		logger.Printf("[INFO] using PGO profile %s", b.PgoProfile)
		cmd.Args = append(cmd.Args, "-pgo="+absPgoProfile)
	}
	if err != nil {
		return err
	}
	if b.Debug {
		// support dlv
		cmd.Args = append(cmd.Args, "-gcflags", "all=-N -l")
	} else {
		if buildEnv.buildFlags == "" {
			cmd.Args = append(cmd.Args,
				"-ldflags", "-w -s", // trim debug symbols
				"-trimpath",
				"-tags", "nobadger,nomysql,nopgx",
			)
		}
	}

	if b.RaceDetector {
		cmd.Args = append(cmd.Args, "-race")
	}
	cmd.Env = env
	err = buildEnv.runCommand(ctx, cmd)
	if err != nil {
		return err
	}

	logger.Printf("[INFO] Build complete: %s", outputFile)

	return nil
}

// setEnv sets an environment variable-value pair in
// env, overriding an existing variable if it already
// exists. The env slice is one such as is returned
// by os.Environ(), and set must also have the form
// of key=value.
func setEnv(env []string, set string) []string {
	parts := strings.SplitN(set, "=", 2)
	key := parts[0]
	for i := 0; i < len(env); i++ {
		if strings.HasPrefix(env[i], key+"=") {
			env[i] = set
			return env
		}
	}
	return append(env, set)
}

// Dependency pairs a Go module path with a version.
type Dependency struct {
	// The name (import path) of the Go package. If at a version > 1,
	// it should contain semantic import version (i.e. "/v2").
	// Used with `go get`.
	PackagePath string `json:"module_path,omitempty"`

	// The version of the Go module, as used with `go get`.
	Version string `json:"version,omitempty"`
}

func (d Dependency) String() string {
	if d.Version != "" {
		return d.PackagePath + "@" + d.Version
	}
	return d.PackagePath
}

// ReplacementPath represents an old or new path component
// within a Go module replacement directive.
type ReplacementPath string

// Param reformats a go.mod replace directive to be
// compatible with the `go mod edit` command.
func (r ReplacementPath) Param() string {
	return strings.Replace(string(r), " ", "@", 1)
}

func (r ReplacementPath) String() string { return string(r) }

// Replace represents a Go module replacement.
type Replace struct {
	// The import path of the module being replaced.
	Old ReplacementPath `json:"old,omitempty"`

	// The path to the replacement module.
	New ReplacementPath `json:"new,omitempty"`
}

// NewReplace creates a new instance of Replace provided old and
// new Go module paths
func NewReplace(old, new string) Replace {
	return Replace{
		Old: ReplacementPath(old),
		New: ReplacementPath(new),
	}
}

// newTempFolder creates a new folder in a temporary location.
// It is the caller's responsibility to remove the folder when finished.
func newTempFolder() (string, error) {
	var parentDir string
	if runtime.GOOS == "darwin" {
		// After upgrading to macOS High Sierra, Caddy builds mysteriously
		// started missing the embedded version information that -ldflags
		// was supposed to produce. But it only happened on macOS after
		// upgrading to High Sierra, and it didn't happen with the usual
		// `go run build.go` -- only when using a buildenv. Bug in git?
		// Nope. Not a bug in Go 1.10 either. Turns out it's a breaking
		// change in macOS High Sierra. When the GOPATH of the buildenv
		// was set to some other folder, like in the $HOME dir, it worked
		// fine. Only within $TMPDIR it broke. The $TMPDIR folder is inside
		// /var, which is symlinked to /private/var, which is mounted
		// with noexec. I don't understand why, but evidently that
		// makes -ldflags of `go build` not work. Bizarre.
		// The solution, I guess, is to just use our own "temp" dir
		// outside of /var. Sigh... as long as it still gets cleaned up,
		// I guess it doesn't matter too much.
		// See: https://github.com/caddyserver/caddy/issues/2036
		// and https://twitter.com/mholt6/status/978345803365273600 (thread)
		// (using an absolute path prevents problems later when removing this
		// folder if the CWD changes)
		var err error
		parentDir, err = filepath.Abs(".")
		if err != nil {
			return "", err
		}
	}
	ts := time.Now().Format(yearMonthDayHourMin)
	return os.MkdirTemp(parentDir, fmt.Sprintf("buildenv_%s.", ts))
}

// versionedModulePath helps enforce Go Module's Semantic Import Versioning (SIV) by
// returning the form of modulePath with the major component of moduleVersion added,
// if > 1. For example, inputs of "foo" and "v1.0.0" will return "foo", but inputs
// of "foo" and "v2.0.0" will return "foo/v2", for use in Go imports and go commands.
// Inputs that conflict, like "foo/v2" and "v3.1.0" are an error. This function
// returns the input if the moduleVersion is not a valid semantic version string.
// If moduleVersion is empty string, the input modulePath is returned without error.
func versionedModulePath(modulePath, moduleVersion string) (string, error) {
	if moduleVersion == "" {
		return modulePath, nil
	}
	ver, err := semver.StrictNewVersion(strings.TrimPrefix(moduleVersion, "v"))
	if err != nil {
		// only return the error if we know they were trying to use a semantic version
		// (could have been a commit SHA or something)
		if strings.HasPrefix(moduleVersion, "v") {
			return "", fmt.Errorf("%s: %v", moduleVersion, err)
		}
		return modulePath, nil
	}
	major := ver.Major()

	// see if the module path has a major version at the end (SIV)
	matches := moduleVersionRegexp.FindStringSubmatch(modulePath)
	if len(matches) == 2 {
		modPathVer, err := strconv.Atoi(matches[1])
		if err != nil {
			return "", fmt.Errorf("this error should be impossible, but module path %s has bad version: %v", modulePath, err)
		}
		if modPathVer != int(major) {
			return "", fmt.Errorf("versioned module path (%s) and requested module major version (%d) diverge", modulePath, major)
		}
	} else if major > 1 {
		modulePath += fmt.Sprintf("/v%d", major)
	}

	return path.Clean(modulePath), nil
}

var moduleVersionRegexp = regexp.MustCompile(`.+/v(\d+)$`)

const (
	// yearMonthDayHourMin is the date format
	// used for temporary folder paths.
	yearMonthDayHourMin = "2006-01-02-1504"

	defaultCaddyModulePath = "github.com/caddyserver/caddy"
)
