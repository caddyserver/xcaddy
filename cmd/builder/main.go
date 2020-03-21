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
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/caddyserver/builder"
)

func init() {
	flag.StringVar(&caddyVersion, "version", "", "The version of Caddy core")
	flag.StringVar(&output, "output", "", "Where to save the resulting binary")
}

func main() {
	flag.Parse()

	if caddyVersion == "" {
		log.Fatal("[FATAL] Caddy version is required (use --version)")
	}

	// assemble list of plugins and their versions
	var plugins []builder.CaddyPlugin
	for _, arg := range flag.Args() {
		parts := strings.SplitN(arg, "@", 2)
		if len(parts) != 2 {
			log.Fatalf("[FATAL] %s: Plugins must be defined using 'module@version' format, similar to the `go get` command", arg)
		}
		plugins = append(plugins, builder.CaddyPlugin{
			ModulePath: parts[0],
			Version:    parts[1],
		})
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
	err := builder.Build(caddyVersion, plugins, output)
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
}

var (
	caddyVersion string
	plugins      []string
	output       string
)
