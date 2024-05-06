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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/caddyserver/xcaddy/internal/utils"
	"github.com/google/shlex"
)

func (b Builder) newEnvironment(ctx context.Context) (*environment, error) {
	// assume Caddy v2 if no semantic version is provided
	caddyModulePath := defaultCaddyModulePath
	if !strings.HasPrefix(b.CaddyVersion, "v") || !strings.Contains(b.CaddyVersion, ".") {
		caddyModulePath += "/v2"
	}
	caddyModulePath, err := versionedModulePath(caddyModulePath, b.CaddyVersion)
	if err != nil {
		return nil, err
	}

	// clean up any SIV-incompatible module paths real quick
	for i, p := range b.Plugins {
		b.Plugins[i].PackagePath, err = versionedModulePath(p.PackagePath, p.Version)
		if err != nil {
			return nil, err
		}
	}

	// create the context for the main module template
	tplCtx := goModTemplateContext{
		CaddyModule: caddyModulePath,
	}
	for _, p := range b.Plugins {
		tplCtx.Plugins = append(tplCtx.Plugins, p.PackagePath)
	}

	// evaluate the template for the main module
	var buf bytes.Buffer
	tpl, err := template.New("main").Parse(mainModuleTemplate)
	if err != nil {
		return nil, err
	}
	err = tpl.Execute(&buf, tplCtx)
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
	log.Printf("[INFO] Writing main module: %s\n%s", mainPath, buf.Bytes())
	err = os.WriteFile(mainPath, buf.Bytes(), 0o644)
	if err != nil {
		return nil, err
	}

	if len(b.EmbedDirs) > 0 {
		for _, d := range b.EmbedDirs {
			err = copy(d.Dir, filepath.Join(tempFolder, "files", d.Name))
			if err != nil {
				return nil, err
			}
			_, err = os.Stat(d.Dir)
			if err != nil {
				return nil, fmt.Errorf("embed directory does not exist: %s", d.Dir)
			}
			log.Printf("[INFO] Embedding directory: %s", d.Dir)
			buf.Reset()
			tpl, err = template.New("embed").Parse(embeddedModuleTemplate)
			if err != nil {
				return nil, err
			}
			err = tpl.Execute(&buf, tplCtx)
			if err != nil {
				return nil, err
			}
			log.Printf("[INFO] Writing 'embedded' module: %s\n%s", mainPath, buf.Bytes())
			emedPath := filepath.Join(tempFolder, "embed.go")
			err = os.WriteFile(emedPath, buf.Bytes(), 0o644)
			if err != nil {
				return nil, err
			}
		}
	}

	env := &environment{
		caddyVersion:    b.CaddyVersion,
		plugins:         b.Plugins,
		caddyModulePath: caddyModulePath,
		tempFolder:      tempFolder,
		timeoutGoGet:    b.TimeoutGet,
		skipCleanup:     b.SkipCleanup,
		buildFlags:      b.BuildFlags,
		modFlags:        b.ModFlags,
	}

	// initialize the go module
	log.Println("[INFO] Initializing Go module")
	cmd := env.newGoModCommand(ctx, "init")
	cmd.Args = append(cmd.Args, "caddy")
	err = env.runCommand(ctx, cmd)
	if err != nil {
		return nil, err
	}

	// specify module replacements before pinning versions
	replaced := make(map[string]string)
	for _, r := range b.Replacements {
		log.Printf("[INFO] Replace %s => %s", r.Old.String(), r.New.String())
		replaced[r.Old.String()] = r.New.String()
	}
	if len(replaced) > 0 {
		cmd := env.newGoModCommand(ctx, "edit")
		for o, n := range replaced {
			cmd.Args = append(cmd.Args, "-replace", fmt.Sprintf("%s=%s", o, n))
		}
		err := env.runCommand(ctx, cmd)
		if err != nil {
			return nil, err
		}
	}

	// check for early abort
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// The timeout for the `go get` command may be different than `go build`,
	// so create a new context with the timeout for `go get`
	if env.timeoutGoGet > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), env.timeoutGoGet)
		defer cancel()
	}

	// pin versions by populating go.mod, first for Caddy itself and then plugins
	log.Println("[INFO] Pinning versions")
	err = env.execGoGet(ctx, caddyModulePath, env.caddyVersion, "", "")
	if err != nil {
		return nil, err
	}
nextPlugin:
	for _, p := range b.Plugins {
		// if module is locally available, do not "go get" it;
		// also note that we iterate and check prefixes, because
		// a plugin package may be a subfolder of a module, i.e.
		// foo/a/plugin is within module foo/a.
		for repl := range replaced {
			if strings.HasPrefix(p.PackagePath, repl) {
				continue nextPlugin
			}
		}
		// also pass the Caddy version to prevent it from being upgraded
		err = env.execGoGet(ctx, p.PackagePath, p.Version, caddyModulePath, env.caddyVersion)
		if err != nil {
			return nil, err
		}
		// check for early abort
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	// doing an empty "go get -d" can potentially resolve some
	// ambiguities introduced by one of the plugins;
	// see https://github.com/caddyserver/xcaddy/pull/92
	err = env.execGoGet(ctx, "", "", "", "")
	if err != nil {
		return nil, err
	}

	log.Println("[INFO] Build environment ready")
	return env, nil
}

type environment struct {
	caddyVersion    string
	plugins         []Dependency
	caddyModulePath string
	tempFolder      string
	timeoutGoGet    time.Duration
	skipCleanup     bool
	buildFlags      string
	modFlags        string
}

// Close cleans up the build environment, including deleting
// the temporary folder from the disk.
func (env environment) Close() error {
	if env.skipCleanup {
		log.Printf("[INFO] Skipping cleanup as requested; leaving folder intact: %s", env.tempFolder)
		return nil
	}
	log.Printf("[INFO] Cleaning up temporary folder: %s", env.tempFolder)
	return os.RemoveAll(env.tempFolder)
}

func (env environment) newCommand(ctx context.Context, command string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = env.tempFolder
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// newGoBuildCommand creates a new *exec.Cmd which assumes the first element in `args` is one of: build, clean, get, install, list, run, or test. The
// created command will also have the value of `XCADDY_GO_BUILD_FLAGS` appended to its arguments, if set.
func (env environment) newGoBuildCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := env.newCommand(ctx, utils.GetGo(), args...)
	return parseAndAppendFlags(cmd, env.buildFlags)
}

// newGoModCommand creates a new *exec.Cmd which assumes `args` are the args for `go mod` command. The
// created command will also have the value of `XCADDY_GO_MOD_FLAGS` appended to its arguments, if set.
func (env environment) newGoModCommand(ctx context.Context, args ...string) *exec.Cmd {
	args = append([]string{"mod"}, args...)
	cmd := env.newCommand(ctx, utils.GetGo(), args...)
	return parseAndAppendFlags(cmd, env.modFlags)
}

func parseAndAppendFlags(cmd *exec.Cmd, flags string) *exec.Cmd {
	if strings.TrimSpace(flags) == "" {
		return cmd
	}

	fs, err := shlex.Split(flags)
	if err != nil {
		log.Printf("[ERROR] Splitting arguments failed: %s", flags)
		return cmd
	}
	cmd.Args = append(cmd.Args, fs...)

	return cmd
}

func (env environment) runCommand(ctx context.Context, cmd *exec.Cmd) error {
	deadline, ok := ctx.Deadline()
	var timeout time.Duration
	// context doesn't necessarily have a deadline
	if ok {
		timeout = time.Until(deadline)
	}
	log.Printf("[INFO] exec (timeout=%s): %+v ", timeout, cmd)

	// start the command; if it fails to start, report error immediately
	err := cmd.Start()
	if err != nil {
		return err
	}

	// wait for the command in a goroutine; the reason for this is
	// very subtle: if, in our select, we do `case cmdErr := <-cmd.Wait()`,
	// then that case would be chosen immediately, because cmd.Wait() is
	// immediately available (even though it blocks for potentially a long
	// time, it can be evaluated immediately). So we have to remove that
	// evaluation from the `case` statement.
	cmdErrChan := make(chan error)
	go func() {
		cmdErrChan <- cmd.Wait()
	}()

	// unblock either when the command finishes, or when the done
	// channel is closed -- whichever comes first
	select {
	case cmdErr := <-cmdErrChan:
		// process ended; report any error immediately
		return cmdErr
	case <-ctx.Done():
		// context was canceled, either due to timeout or
		// maybe a signal from higher up canceled the parent
		// context; presumably, the OS also sent the signal
		// to the child process, so wait for it to die
		select {
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		case <-cmdErrChan:
		}
		return ctx.Err()
	}
}

// execGoGet runs "go get -d -v" with the given module/version as an argument.
// Also allows passing in a second module/version pair, meant to be the main
// Caddy module/version we're building against; this will prevent the
// plugin module from causing the Caddy version to upgrade, if the plugin
// version requires a newer version of Caddy.
// See https://github.com/caddyserver/xcaddy/issues/54
func (env environment) execGoGet(ctx context.Context, modulePath, moduleVersion, caddyModulePath, caddyVersion string) error {
	mod := modulePath
	if moduleVersion != "" {
		mod += "@" + moduleVersion
	}
	caddy := caddyModulePath
	if caddyVersion != "" {
		caddy += "@" + caddyVersion
	}

	cmd := env.newGoBuildCommand(ctx, "get", "-d", "-v")
	// using an empty string as an additional argument to "go get"
	// breaks the command since it treats the empty string as a
	// distinct argument, so we're using an if statement to avoid it.
	if caddy != "" {
		cmd.Args = append(cmd.Args, mod, caddy)
	} else {
		cmd.Args = append(cmd.Args, mod)
	}

	return env.runCommand(ctx, cmd)
}

type goModTemplateContext struct {
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

// originally published in: https://github.com/mholt/caddy-embed
const embeddedModuleTemplate = `package main

import (
	"embed"
	"io/fs"
	"strings"

	"{{.CaddyModule}}"
	"{{.CaddyModule}}/caddyconfig/caddyfile"
)

// embedded is what will contain your static files. The go command
// will automatically embed the files subfolder into this virtual
// file system. You can optionally change the go:embed directive
// to embed other files or folders.
//
//go:embed files
var embedded embed.FS

// files is the actual, more generic file system to be utilized.
var files fs.FS = embedded

// topFolder is the name of the top folder of the virtual
// file system. go:embed does not let us add the contents
// of a folder to the root of a virtual file system, so
// if we want to trim that root folder prefix, we need to
// also specify it in code as a string. Otherwise the
// user would need to add configuration or code to trim
// this root prefix from all filenames, e.g. specifying
// "root files" in their file_server config.
//
// It is NOT REQUIRED to change this if changing the
// go:embed directive; it is just for convenience in
// the default case.
const topFolder = "files"

func init() {
	caddy.RegisterModule(FS{})
	stripFolderPrefix()
}

// stripFolderPrefix opens the root of the file system. If it
// contains only 1 file, being a directory with the same
// name as the topFolder const, then the file system will
// be fs.Sub()'ed so the contents of the top folder can be
// accessed as if they were in the root of the file system.
// This is a convenience so most users don't have to add
// additional configuration or prefix their filenames
// unnecessarily.
func stripFolderPrefix() error {
	if f, err := files.Open("."); err == nil {
		defer f.Close()

		if dir, ok := f.(fs.ReadDirFile); ok {
			entries, err := dir.ReadDir(2)
			if err == nil &&
				len(entries) == 1 &&
				entries[0].IsDir() &&
				entries[0].Name() == topFolder {
				if sub, err := fs.Sub(embedded, topFolder); err == nil {
					files = sub
				}
			}
		}
	}
	return nil
}

// FS implements a Caddy module and fs.FS for an embedded
// file system provided by an unexported package variable.
//
// To use, simply put your files in a subfolder called
// "files", then build Caddy with your local copy of this
// plugin. Your site's files will be embedded directly
// into the binary.
//
// If the embedded file system contains only one file in
// its root which is a folder named "files", this module
// will strip that folder prefix using fs.Sub(), so that
// the contents of the folder can be accessed by name as
// if they were in the actual root of the file system.
// In other words, before: files/foo.txt, after: foo.txt.
type FS struct{}

// CaddyModule returns the Caddy module information.
func (FS) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.fs.embedded",
		New: func() caddy.Module { return new(FS) },
	}
}

func (FS) Open(name string) (fs.File, error) {
	// TODO: the file server doesn't clean up leading and trailing slashes, but embed.FS is particular so we remove them here; I wonder if the file server should be tidy in the first place
	name = strings.Trim(name, "/")
	return files.Open(name)
}

// UnmarshalCaddyfile exists so this module can be used in
// the Caddyfile, but there is nothing to unmarshal.
func (FS) UnmarshalCaddyfile(d *caddyfile.Dispenser) error { return nil }

// Interface guards
var (
	_ fs.FS                 = (*FS)(nil)
	_ caddyfile.Unmarshaler = (*FS)(nil)
)
`
