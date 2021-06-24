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

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/k6io/xk6"
)

var (
	k6Version    = os.Getenv("K6_VERSION")
	k6Repo       = os.Getenv("XK6_K6_REPO")
	raceDetector = os.Getenv("XK6_RACE_DETECTOR") == "1"
	skipCleanup  = os.Getenv("XK6_SKIP_CLEANUP") == "1"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go trapSignals(ctx, cancel)

	if len(os.Args) > 1 && os.Args[1] == "build" {
		if err := runBuild(ctx, os.Args[2:]); err != nil {
			log.Fatalf("[ERROR] %v", err)
		}
		return
	}

	if err := runDev(ctx, os.Args[1:]); err != nil {
		log.Fatalf("[ERROR] %v", err)
	}
}

func runBuild(ctx context.Context, args []string) error {
	// parse the command line args... rather primitively
	var argK6Version, output string
	var extensions []xk6.Dependency
	var replacements []xk6.Replace
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--with":
			if i == len(args)-1 {
				return fmt.Errorf("expected value after --with flag")
			}
			i++
			mod, ver, repl, err := splitWith(args[i])
			if err != nil {
				return err
			}
			mod = strings.TrimSuffix(mod, "/") // easy to accidentally leave a trailing slash if pasting from a URL, but is invalid for Go modules
			extensions = append(extensions, xk6.Dependency{
				PackagePath: mod,
				Version:     ver,
			})
			if repl != "" {
				if repl == "." {
					if cwd, err := os.Getwd(); err != nil {
						return err
					} else {
						repl = cwd
					}
				}
				replacements = append(replacements, xk6.NewReplace(mod, repl))
			}

		case "--output":
			if i == len(args)-1 {
				return fmt.Errorf("expected value after --output flag")
			}
			i++
			output = args[i]

		default:
			if argK6Version != "" {
				return fmt.Errorf("missing flag; k6 version already set at %s", argK6Version)
			}
			argK6Version = args[i]
		}
	}

	// prefer k6 version from command line argument over env var
	if argK6Version != "" {
		k6Version = argK6Version
	}

	// ensure an output file is always specified
	if output == "" {
		output = getK6OutputFile()
	}

	// perform the build
	builder := xk6.Builder{
		Compile: xk6.Compile{
			Cgo: os.Getenv("CGO_ENABLED") == "1",
		},
		K6Repo:       k6Repo,
		K6Version:    k6Version,
		Extensions:   extensions,
		Replacements: replacements,
		RaceDetector: raceDetector,
		SkipCleanup:  skipCleanup,
	}
	err := builder.Build(ctx, output)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	// prove the build is working by printing the version
	if runtime.GOOS == os.Getenv("GOOS") && runtime.GOARCH == os.Getenv("GOARCH") {
		if !filepath.IsAbs(output) {
			output = "." + string(filepath.Separator) + output
		}
		fmt.Println()
		fmt.Printf("%s version\n", output)
		cmd := exec.Command(output, "version")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Fatalf("[FATAL] %v", err)
		}
	}

	return nil
}

func getK6OutputFile() string {
	if runtime.GOOS == "windows" {
		return ".\\k6.exe"
	}
	return "./k6"
}

func runDev(ctx context.Context, args []string) error {
	binOutput := getK6OutputFile()

	// get current/main module name
	cmd := exec.Command("go", "list", "-m")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("exec %v: %v: %s", cmd.Args, err, string(out))
	}
	currentModule := strings.TrimSpace(string(out))

	// get the root directory of the main module
	cmd = exec.Command("go", "list", "-m", "-f={{.Dir}}")
	cmd.Stderr = os.Stderr
	out, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("exec %v: %v: %s", cmd.Args, err, string(out))
	}
	moduleDir := strings.TrimSpace(string(out))

	// make sure the module being developed is replaced
	// so that the local copy is used
	replacements := []xk6.Replace{
		xk6.NewReplace(currentModule, moduleDir),
	}

	// replace directives only apply to the top-level/main go.mod,
	// and since this tool is a carry-through for the user's actual
	// go.mod, we need to transfer their replace directives through
	// to the one we're making
	cmd = exec.Command("go", "list", "-mod=readonly", "-m", "-f={{if .Replace}}{{.Path}} => {{.Replace}}{{end}}", "all")
	cmd.Stderr = os.Stderr
	out, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("exec %v: %v: %s", cmd.Args, err, string(out))
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(line, "=>")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		replacements = append(replacements, xk6.NewReplace(
			strings.TrimSpace(parts[0]),
			strings.TrimSpace(parts[1]),
		))
	}

	// reconcile remaining path segments; for example if a module foo/a
	// is rooted at directory path /home/foo/a, but the current directory
	// is /home/foo/a/b, then the package to import should be foo/a/b
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current directory: %v", err)
	}
	importPath := normalizeImportPath(currentModule, cwd, moduleDir)

	// build k6 with this module plugged in
	builder := xk6.Builder{
		Compile: xk6.Compile{
			Cgo: os.Getenv("CGO_ENABLED") == "1",
		},
		K6Repo:    k6Repo,
		K6Version: k6Version,
		Extensions: []xk6.Dependency{
			{PackagePath: importPath},
		},
		Replacements: replacements,
		RaceDetector: raceDetector,
		SkipCleanup:  skipCleanup,
	}
	err = builder.Build(ctx, binOutput)
	if err != nil {
		return err
	}

	log.Printf("[INFO] Running %v\n\n", append([]string{binOutput}, args...))

	cmd = exec.Command(binOutput, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		return err
	}
	defer func() {
		if skipCleanup {
			log.Printf("[INFO] Skipping cleanup as requested; leaving artifact: %s", binOutput)
			return
		}
		err = os.Remove(binOutput)
		if err != nil && !os.IsNotExist(err) {
			log.Printf("[ERROR] Deleting temporary binary %s: %v", binOutput, err)
		}
	}()

	return cmd.Wait()
}

func normalizeImportPath(currentModule, cwd, moduleDir string) string {
	return path.Join(currentModule, filepath.ToSlash(strings.TrimPrefix(cwd, moduleDir)))
}

func trapSignals(ctx context.Context, cancel context.CancelFunc) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		log.Printf("[INFO] SIGINT: Shutting down")
		cancel()
	case <-ctx.Done():
		return
	}
}

func splitWith(arg string) (module, version, replace string, err error) {
	const versionSplit, replaceSplit = "@", "="

	parts := strings.SplitN(arg, versionSplit, 2)
	module = parts[0]

	if len(parts) == 1 {
		parts := strings.SplitN(module, replaceSplit, 2)
		if len(parts) > 1 {
			module = parts[0]
			replace = parts[1]
		}
	} else {
		version = parts[1]
		parts := strings.SplitN(version, replaceSplit, 2)
		if len(parts) > 1 {
			version = parts[0]
			replace = parts[1]
		}
	}

	if module == "" {
		err = fmt.Errorf("module name is required")
	}

	return
}
