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
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func newEnvironment(caddyVersion string, plugins []Dependency) (*environment, error) {
	// assume v2 if no semantic version is provided
	caddyModulePath := defaultCaddyModulePath
	if !strings.HasPrefix(caddyVersion, "v") || !strings.Contains(caddyVersion, ".") {
		caddyModulePath += "/v2"
	}
	caddyModulePath, err := versionedModulePath(caddyModulePath, caddyVersion)
	if err != nil {
		return nil, err
	}

	// clean up any SIV-incompatible module paths real quick
	for i, p := range plugins {
		plugins[i].ModulePath, err = versionedModulePath(p.ModulePath, p.Version)
		if err != nil {
			return nil, err
		}
	}

	// create the context for the main module template
	ctx := moduleTemplateContext{
		CaddyModule: caddyModulePath,
	}
	for _, p := range plugins {
		ctx.Plugins = append(ctx.Plugins, p.ModulePath)
	}

	// evaluate the template for the main module
	var buf bytes.Buffer
	tpl, err := template.New("main").Parse(mainModuleTemplate)
	if err != nil {
		return nil, err
	}
	err = tpl.Execute(&buf, ctx)
	if err != nil {
		return nil, err
	}

	// create the folder in which the build environment will operate
	tempFolder, err := newTempFolder()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err2 := os.RemoveAll(tempFolder)
			if err2 != nil {
				err = fmt.Errorf("%w; additionally, cleaning up folder: %v", err, err2)
			}
		}
	}()
	log.Printf("[INFO] Temporary folder: %s", tempFolder)

	// write the main module file to temporary folder
	mainPath := filepath.Join(tempFolder, "main.go")
	log.Printf("[INFO] Writing main module: %s", mainPath)
	err = ioutil.WriteFile(mainPath, buf.Bytes(), 0644)
	if err != nil {
		return nil, err
	}

	env := &environment{
		caddyVersion:    caddyVersion,
		plugins:         plugins,
		caddyModulePath: caddyModulePath,
		tempFolder:      tempFolder,
	}

	// initialize the go module
	log.Println("[INFO] Initializing Go module")
	cmd := env.newCommand("go", "mod", "init", "caddy")
	err = env.runCommand(cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}

	// specify module replacements before pinning versions
	for _, p := range plugins {
		if p.Replace != "" {
			log.Printf("[INFO] Replace %s => %s", p.ModulePath, p.Replace)
			cmd := env.newCommand("go", "mod", "edit",
				"-replace", fmt.Sprintf("%s=%s", p.ModulePath, p.Replace))
			err := env.runCommand(cmd, 10*time.Second)
			if err != nil {
				return nil, err
			}
		}
	}

	// pin versions by populating go.mod, first for Caddy itself and then plugins
	log.Println("[INFO] Pinning versions")
	err = env.execGoGet(caddyModulePath, caddyVersion)
	if err != nil {
		return nil, err
	}
	for _, p := range plugins {
		if p.Replace != "" {
			continue
		}
		err = env.execGoGet(p.ModulePath, p.Version)
		if err != nil {
			return nil, err
		}
	}

	log.Println("[INFO] Build environment ready")

	return env, nil
}

type environment struct {
	caddyVersion    string
	plugins         []Dependency
	caddyModulePath string
	tempFolder      string
}

// Close cleans up the build environment, including deleting
// the temporary folder from the disk.
func (env environment) Close() error {
	log.Printf("[INFO] Cleaning up temporary folder: %s", env.tempFolder)
	return os.RemoveAll(env.tempFolder)
}

func (env environment) newCommand(command string, args ...string) *exec.Cmd {
	cmd := exec.Command(command, args...)
	cmd.Dir = env.tempFolder
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func (env environment) runCommand(cmd *exec.Cmd, timeout time.Duration) error {
	log.Printf("[INFO] exec (timeout=%s): %+v ", timeout, cmd)

	// no timeout? this is easy; just run it
	if timeout == 0 {
		return cmd.Run()
	}

	// otherwise start it and use a timer
	err := cmd.Start()
	if err != nil {
		return err
	}
	timer := time.AfterFunc(timeout, func() {
		err = fmt.Errorf("timed out (builder-enforced)")
		cmd.Process.Kill()
	})
	waitErr := cmd.Wait()
	timer.Stop()
	if err != nil {
		return err
	}
	return waitErr
}

func (env environment) execGoGet(modulePath, moduleVersion string) error {
	mod := modulePath + "@" + moduleVersion
	cmd := env.newCommand("go", "get", "-d", "-v", mod)
	return env.runCommand(cmd, 60*time.Second)
}

type moduleTemplateContext struct {
	CaddyModule string
	Plugins     []string
}

const mainModuleTemplate = `package main

import (
	caddycmd "{{.CaddyModule}}/cmd"

	// plug in Caddy modules here
	_ "{{.CaddyModule}}/modules/standard"
	{{- range .Plugins}}
	_ "{{.}}"
	{{- end}}
)

func main() {
	caddycmd.Main()
}
`
