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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/caddyserver/xcaddy"
	"github.com/caddyserver/xcaddy/internal/utils"
)

var (
	caddyVersion     = os.Getenv("CADDY_VERSION")
	raceDetector     = os.Getenv("XCADDY_RACE_DETECTOR") == "1"
	skipBuild        = os.Getenv("XCADDY_SKIP_BUILD") == "1"
	skipCleanup      = os.Getenv("XCADDY_SKIP_CLEANUP") == "1" || skipBuild
	buildDebugOutput = os.Getenv("XCADDY_DEBUG") == "1"
	buildFlags       = os.Getenv("XCADDY_GO_BUILD_FLAGS")
	modFlags         = os.Getenv("XCADDY_GO_MOD_FLAGS")
)

func Main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go trapSignals(ctx, cancel)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getCaddyOutputFile() string {
	f := "." + string(filepath.Separator) + "caddy"
	// compiling for Windows or compiling on windows without setting GOOS, use .exe extension
	if utils.GetGOOS() == "windows" {
		f += ".exe"
	}
	return f
}

func setcapIfRequested(output string) error {
	if os.Getenv("XCADDY_SETCAP") != "1" {
		return nil
	}

	args := []string{"setcap", "cap_net_bind_service=+ep", output}

	// check if sudo isn't available, or we were instructed not to use it
	_, sudoNotFound := exec.LookPath("sudo")
	skipSudo := sudoNotFound != nil || os.Getenv("XCADDY_SUDO") == "0"

	var cmd *exec.Cmd
	if skipSudo {
		cmd = exec.Command(args[0], args[1:]...)
	} else {
		cmd = exec.Command("sudo", args...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("[INFO] Setting capabilities (requires admin privileges): %v", cmd.Args)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to setcap on the binary: %v", err)
	}

	return nil
}

type module struct {
	Path    string  // module path
	Version string  // module version
	Replace *module // replaced by this module
	Main    bool    // is this the main module?
	Dir     string  // directory holding files for this module, if any
}

func parseGoListJson(out []byte) (currentModule, moduleDir string, replacements []xcaddy.Replace, err error) {
	var unjoinedReplaces []int

	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var mod module
		if err = decoder.Decode(&mod); err == io.EOF {
			err = nil
			break
		} else if err != nil {
			return
		}

		if mod.Main {
			// Current module is main module, retrieve the main module name and
			// root directory path of the main module
			currentModule = mod.Path
			moduleDir = mod.Dir
			replacements = append(replacements, xcaddy.NewReplace(currentModule, moduleDir))
			continue
		}

		// Skip if current module is not replacement
		if mod.Replace == nil {
			continue
		}

		// 1. Target is module, version is required in this case
		// 2A. Target is absolute path
		// 2B. Target is relative path, proper handling is required in this case
		dstPath := mod.Replace.Path
		dstVersion := mod.Replace.Version
		var dst string
		if dstVersion != "" {
			dst = dstPath + "@" + dstVersion
		} else if filepath.IsAbs(dstPath) {
			dst = dstPath
		} else {
			if moduleDir != "" {
				dst = filepath.Join(moduleDir, dstPath)
				log.Printf("[INFO] Resolved relative replacement %s to %s", dstPath, dst)
			} else {
				// moduleDir is not parsed yet, defer to later
				dst = dstPath
				unjoinedReplaces = append(unjoinedReplaces, len(replacements))
			}
		}

		replacements = append(replacements, xcaddy.NewReplace(mod.Path, dst))
	}
	for _, idx := range unjoinedReplaces {
		unresolved := string(replacements[idx].New)
		resolved := filepath.Join(moduleDir, unresolved)
		log.Printf("[INFO] Resolved previously-unjoined relative replacement %s to %s", unresolved, resolved)
		replacements[idx].New = xcaddy.ReplacementPath(resolved)
	}
	return
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

	parts := strings.SplitN(arg, replaceSplit, 2)
	if len(parts) > 1 {
		replace = parts[1]
	}
	module = parts[0]

	// accommodate module paths that have @ in them, but we can only tolerate that if there's also
	// a version, otherwise we don't know if it's a version separator or part of the file path (see #109)
	lastVersionSplit := strings.LastIndex(module, versionSplit)
	if lastVersionSplit < 0 {
		if replaceIdx := strings.Index(module, replaceSplit); replaceIdx >= 0 {
			module, replace = module[:replaceIdx], module[replaceIdx+1:]
		}
	} else {
		module, version = module[:lastVersionSplit], module[lastVersionSplit+1:]
		if replaceIdx := strings.Index(version, replaceSplit); replaceIdx >= 0 {
			version, replace = module[:replaceIdx], module[replaceIdx+1:]
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
