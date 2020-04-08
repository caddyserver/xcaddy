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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/caddyserver/xcaddy"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "build" {
		if err := runBuild(os.Args[2:]); err != nil {
			log.Fatalf("[ERROR] %v", err)
		}
		return
	}

	// TODO: the caddy version needs to be settable by the user... maybe an env var?
	if err := runDev("v2.0.0-rc.1", os.Args[1:]); err != nil {
		log.Fatalf("[ERROR] %v", err)
	}
}

func runBuild(args []string) error {
	// parse the command line args... rather primitively
	var caddyVersion, output string
	var plugins []xcaddy.Dependency
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--with":
			if i == len(args)-1 {
				return fmt.Errorf("expected value after --with flag")
			}
			i++
			var mod, ver string
			arg := args[i]
			parts := strings.SplitN(arg, "@", 2)
			mod = parts[0]
			if len(parts) == 2 {
				ver = parts[1]
			}
			plugins = append(plugins, xcaddy.Dependency{
				ModulePath: mod,
				Version:    ver,
			})

		case "--output":
			if i == len(args)-1 {
				return fmt.Errorf("expected value after --output flag")
			}
			i++
			output = args[i]

		default:
			if caddyVersion != "" {
				return fmt.Errorf("missing flag; caddy version already set at %s", caddyVersion)
			}
			caddyVersion = args[i]
		}
	}

	// ensure an output file is always specified
	if output == "" {
		if runtime.GOOS == "windows" {
			output = "caddy.exe"
		} else {
			output = "caddy"
		}
	}

	// perform the build
	builder := xcaddy.Builder{
		CaddyVersion: caddyVersion,
		Plugins:      plugins,
	}
	err := builder.Build(output)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	// prove the build is working by printing the version
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

	return nil
}

func runDev(caddyVersion string, args []string) error {
	const binOutput = "./caddy"

	// get current/main module name
	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	currentModule := strings.TrimSpace(string(out))

	// get the root directory of the main module
	cmd = exec.Command("go", "list", "-m", "-f={{.Dir}}")
	out, err = cmd.Output()
	if err != nil {
		return err
	}
	moduleDir := strings.TrimSpace(string(out))

	// build caddy with this module plugged in
	builder := xcaddy.Builder{
		CaddyVersion: caddyVersion,
		Plugins: []xcaddy.Dependency{
			{
				ModulePath: currentModule,
				Replace:    moduleDir,
			},
		},
	}
	err = builder.Build(binOutput)
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

	cleanup := func() {
		err = os.Remove(binOutput)
		if err != nil && !os.IsNotExist(err) {
			log.Printf("[ERROR] Deleting temporary binary %s: %v", binOutput, err)
		}
	}
	defer cleanup()
	go func() {
		time.Sleep(2 * time.Second)
		cleanup()
	}()

	return cmd.Wait()
}
