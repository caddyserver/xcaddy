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
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// Builder can produce a custom Caddy build with the
// configuration it represents.
type Builder struct {
	CaddyVersion string       `json:"caddy_version,omitempty"`
	Plugins      []Dependency `json:"plugins,omitempty"`
	Replacements []Replace    `json:"replacements,omitempty"`
}

// Build builds Caddy at the configured version with the
// configured plugins and plops down a binary at outputFile.
func (b Builder) Build(ctx context.Context, outputFile string) error {
	if b.CaddyVersion == "" {
		return fmt.Errorf("CaddyVersion must be set")
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

	env, err := b.newEnvironment(ctx)
	if err != nil {
		return err
	}
	defer env.Close()

	log.Println("[INFO] Building Caddy")

	cmd := env.newCommand("go", "build",
		"-o", absOutputFile,
		"-ldflags", "-w -s", // trim debug symbols
		"-trimpath",
	)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	err = env.runCommand(ctx, cmd, 5*time.Minute)
	if err != nil {
		return err
	}

	log.Printf("[INFO] Build complete: %s", outputFile)

	return nil
}

// Dependency pairs a Go module path with a version.
type Dependency struct {
	// The name (path) of the Go module. If at a version > 1, it
	// should contain semantic import version suffix (i.e. "/v2").
	// Used with `go get`
	ModulePath string

	// The version of the Go module, like used with `go get`.
	Version string
}

// Replace represents a Go module replacement.
type Replace struct {
	// The import path of the module being replaced.
	Old string

	// The path to the replacement module.
	New string
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
	return ioutil.TempDir(parentDir, fmt.Sprintf("buildenv_%s.", ts))
}

// versionedModulePath helps enforce Go Module's Semantic Import Versioning (SIV) by
// returning the form of modulePath with the major component of moduleVersion added,
// if > 1. For example, inputs of "foo" and "v1.0.0" will return "foo", but inputs
// of "foo" and "v2.0.0" will return "foo/v2", for use in Go imports and go commands.
// Inputs that conflict, like "foo/v2" and "v3.1.0" are an error. This function
// returns the input if the moduleVersion is not a valid semantic version string.
func versionedModulePath(modulePath, moduleVersion string) (string, error) {
	ver, err := semver.StrictNewVersion(strings.TrimPrefix(moduleVersion, "v"))
	if err != nil {
		// only return the error if we know they were trying to use a semantic version
		// (could have been a commit SHA or something)
		if strings.HasPrefix(moduleVersion, "v") {
			return "", err
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
