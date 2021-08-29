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

package xcaddycmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/caddyserver/xcaddy"
)

var (
	caddyVersion     = os.Getenv("CADDY_VERSION")
	raceDetector     = os.Getenv("XCADDY_RACE_DETECTOR") == "1"
	skipBuild        = os.Getenv("XCADDY_SKIP_BUILD") == "1"
	skipCleanup      = os.Getenv("XCADDY_SKIP_CLEANUP") == "1"
	buildDebugOutput = os.Getenv("XCADDY_DEBUG") == "1"
)

func Main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go trapSignals(ctx, cancel)

	if len(os.Args) > 1 && os.Args[1] == "build" {
		if err := runBuild(ctx, os.Args[2:]); err != nil {
			log.Fatalf("[ERROR] %v", err)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(xcaddyVersion())
		return
	}

	if err := runDev(ctx, os.Args[1:]); err != nil {
		log.Fatalf("[ERROR] %v", err)
	}
}

func runBuild(ctx context.Context, args []string) error {
	// parse the command line args... rather primitively
	var argCaddyVersion, output string
	var plugins []xcaddy.Dependency
	var replacements []xcaddy.Replace
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
			plugins = append(plugins, xcaddy.Dependency{
				PackagePath: mod,
				Version:     ver,
			})
			if repl != "" {
				// adjust relative replacements in current working directory since our temporary module is in a different directory
				if strings.HasPrefix(repl, ".") {
					repl, err = filepath.Abs(repl)
					if err != nil {
						log.Fatalf("[FATAL] %v", err)
					}
					log.Printf("[INFO] Resolved relative replacement %s to %s", args[i], repl)
				}
				replacements = append(replacements, xcaddy.NewReplace(mod, repl))
			}

		case "--output":
			if i == len(args)-1 {
				return fmt.Errorf("expected value after --output flag")
			}
			i++
			output = args[i]

		default:
			if argCaddyVersion != "" {
				return fmt.Errorf("missing flag; caddy version already set at %s", argCaddyVersion)
			}
			argCaddyVersion = args[i]
		}
	}

	// prefer caddy version from command line argument over env var
	if argCaddyVersion != "" {
		caddyVersion = argCaddyVersion
	}

	// ensure an output file is always specified
	if output == "" {
		output = getCaddyOutputFile()
	}

	// perform the build
	builder := xcaddy.Builder{
		Compile: xcaddy.Compile{
			Cgo: os.Getenv("CGO_ENABLED") == "1",
		},
		CaddyVersion: caddyVersion,
		Plugins:      plugins,
		Replacements: replacements,
		RaceDetector: raceDetector,
		SkipBuild:    skipBuild,
		SkipCleanup:  skipCleanup,
		Debug:        buildDebugOutput,
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

func getCaddyOutputFile() string {
	if runtime.GOOS == "windows" {
		return "caddy.exe"
	}
	return "." + string(filepath.Separator) + "caddy"
}

func runDev(ctx context.Context, args []string) error {
	binOutput := getCaddyOutputFile()

	// get current/main module name and the root directory of the main module
	cmd := exec.Command("go", "list", "-m", "-json")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("exec %v: %v: %s", cmd.Args, err, string(out))
	}
	var mainModule map[string]interface{}
	err = json.Unmarshal(out, &mainModule)
	if err != nil {
		return fmt.Errorf("json parse error: %v", err)
	}
	currentModule := mainModule["Path"].(string)
	moduleDir := mainModule["Dir"].(string)

	// make sure the module being developed is replaced
	// so that the local copy is used
	replacements := []xcaddy.Replace{
		xcaddy.NewReplace(currentModule, moduleDir),
	}

	// replace directives only apply to the top-level/main go.mod,
	// and since this tool is a carry-through for the user's actual
	// go.mod, we need to transfer their replace directives through
	// to the one we're making
	cmd = exec.Command("go", "list", "-m", "-f={{if .Replace}}{{.Path}}=>{{.Replace}}{{end}}", "all")
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

		// adjust relative replacements in original module since our temporary module is in a different directory
		if !filepath.IsAbs(parts[1]) {
			parts[1] = filepath.Join(moduleDir, parts[1])
			log.Printf("[INFO] Resolved relative replacement %s to %s", line, parts[1])
		}

		replacements = append(replacements, xcaddy.NewReplace(parts[0], parts[1]))
	}

	// reconcile remaining path segments; for example if a module foo/a
	// is rooted at directory path /home/foo/a, but the current directory
	// is /home/foo/a/b, then the package to import should be foo/a/b
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current directory: %v", err)
	}
	importPath := normalizeImportPath(currentModule, cwd, moduleDir)

	// build caddy with this module plugged in
	builder := xcaddy.Builder{
		Compile: xcaddy.Compile{
			Cgo: os.Getenv("CGO_ENABLED") == "1",
		},
		CaddyVersion: caddyVersion,
		Plugins: []xcaddy.Dependency{
			{PackagePath: importPath},
		},
		Replacements: replacements,
		RaceDetector: raceDetector,
		SkipBuild:    skipBuild,
		SkipCleanup:  skipCleanup,
		Debug:        buildDebugOutput,
	}
	err = builder.Build(ctx, binOutput)
	if err != nil {
		return err
	}

	if os.Getenv("XCADDY_SETCAP") == "1" {
		cmd = exec.Command("sudo", "setcap", "cap_net_bind_service=+ep", binOutput)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		log.Printf("[INFO] Setting capabilities (requires admin privileges): %v", cmd.Args)
		if err = cmd.Run(); err != nil {
			return err
		}
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

	modules := strings.SplitN(arg, replaceSplit, 2)
	if len(modules) > 1 {
		replace = modules[1]
	}

	parts := strings.SplitN(modules[0], versionSplit, 2)
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

// xcaddyVersion returns a detailed version string, if available.
func xcaddyVersion() string {
	mod := goModule()
	ver := mod.Version
	if mod.Sum != "" {
		ver += " " + mod.Sum
	}
	if mod.Replace != nil {
		ver += " => " + mod.Replace.Path
		if mod.Replace.Version != "" {
			ver += "@" + mod.Replace.Version
		}
		if mod.Replace.Sum != "" {
			ver += " " + mod.Replace.Sum
		}
	}
	return ver
}

func goModule() *debug.Module {
	mod := &debug.Module{}
	mod.Version = "unknown"
	bi, ok := debug.ReadBuildInfo()
	if ok {
		mod.Path = bi.Main.Path
		// The recommended way to build xcaddy involves
		// creating a separate main module, which
		// TODO: track related Go issue: https://github.com/golang/go/issues/29228
		// once that issue is fixed, we should just be able to use bi.Main... hopefully.
		for _, dep := range bi.Deps {
			if dep.Path == "github.com/caddyserver/xcaddy" {
				return dep
			}
		}
		return &bi.Main
	}
	return mod
}
