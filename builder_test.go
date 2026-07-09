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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReplacementPath_Param(t *testing.T) {
	tests := []struct {
		name string
		r    ReplacementPath
		want string
	}{
		{
			"Empty",
			ReplacementPath(""),
			"",
		},
		{
			"ModulePath",
			ReplacementPath("github.com/x/y"),
			"github.com/x/y",
		},
		{
			"ModulePath Version Pinned",
			ReplacementPath("github.com/x/y v0.0.0-20200101000000-xxxxxxxxxxxx"),
			"github.com/x/y@v0.0.0-20200101000000-xxxxxxxxxxxx",
		},
		{
			"FilePath",
			ReplacementPath("/x/y/z"),
			"/x/y/z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fmt.Println(tt.r.Param())
			if got := tt.r.Param(); got != tt.want {
				t.Errorf("ReplacementPath.Param() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewReplace(t *testing.T) {
	type args struct {
		old string
		new string
	}
	tests := []struct {
		name string
		args args
		want Replace
	}{
		{
			"Empty",
			args{"", ""},
			Replace{"", ""},
		},
		{
			"Constructor",
			args{"a", "b"},
			Replace{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewReplace(tt.args.old, tt.args.new); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewReplace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildOutputPerStepContainment(t *testing.T) {
	// the callback runs concurrently with the step and reads its
	// output directly; no consumer-side goroutine is involved
	var order []Step
	outputs := make(map[Step]string)
	out := newBuildOutput(func(e *StepEvent) error {
		order = append(order, e.Step)
		data, err := io.ReadAll(e.Output) // returns only at EOF, i.e. when the step ends
		if err != nil {
			return err
		}
		outputs[e.Step] = string(data)
		return nil
	})

	if err := out.beginStep(StepCreateEnvironment); err != nil {
		t.Fatalf("beginStep() error = %v", err)
	}
	out.logger.Print("[INFO] marker-log-environment")
	fmt.Fprint(out.stdout(), "marker-stdout-environment")
	fmt.Fprint(out.stderr(), "marker-stderr-environment")

	if err := out.beginStep(StepCompile); err != nil {
		t.Fatalf("beginStep() error = %v", err)
	}
	out.logger.Print("[INFO] marker-log-compile")
	fmt.Fprint(out.stderr(), "marker-stderr-compile")

	// ending the final step (as Build does when it returns) joins
	// the last callback, after which all results are visible
	if err := out.endStep(); err != nil {
		t.Fatalf("endStep() error = %v", err)
	}

	wantOrder := []Step{StepCreateEnvironment, StepCompile}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("step order = %v, want %v", order, wantOrder)
	}

	envOut := outputs[StepCreateEnvironment]
	for _, want := range []string{
		"marker-log-environment",
		"marker-stdout-environment",
		"marker-stderr-environment",
	} {
		if !strings.Contains(envOut, want) {
			t.Errorf("create_environment output missing %q; got:\n%s", want, envOut)
		}
	}
	if strings.Contains(envOut, "compile") {
		t.Errorf("create_environment output contains later step's output:\n%s", envOut)
	}

	compileOut := outputs[StepCompile]
	for _, want := range []string{"marker-log-compile", "marker-stderr-compile"} {
		if !strings.Contains(compileOut, want) {
			t.Errorf("compile output missing %q; got:\n%s", want, compileOut)
		}
	}
	if strings.Contains(compileOut, "environment") {
		t.Errorf("compile output contains earlier step's output:\n%s", compileOut)
	}
}

func TestBuildOutputNilCallback(t *testing.T) {
	out := newBuildOutput(nil)

	if out.logger != log.Default() {
		t.Error("nil OnStep should use the standard log package's default logger")
	}

	// must be a no-op, not a panic
	if err := out.beginStep(StepCompile); err != nil {
		t.Fatalf("beginStep() error = %v", err)
	}

	// with no step active, facets write to their fallback stream
	var fallback bytes.Buffer
	facet := outputFacet{out, &fallback}
	fmt.Fprint(facet, "marker-default")
	if got := fallback.String(); got != "marker-default" {
		t.Errorf("fallback got %q, want %q", got, "marker-default")
	}
}

func TestBuildOutputUnreadIsDiscarded(t *testing.T) {
	// a callback that returns without reading Output must not
	// block or fail the build: writes are buffered and the buffer
	// is released when the step ends; this test completing at all
	// is the assertion
	out := newBuildOutput(func(e *StepEvent) error { return nil })

	for _, step := range []Step{StepCreateEnvironment, StepPinVersions, StepCompile} {
		if err := out.beginStep(step); err != nil {
			t.Fatalf("beginStep() error = %v", err)
		}
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(out.stderr(), "unread output %d\n", i)
		}
	}
	if err := out.endStep(); err != nil {
		t.Fatalf("endStep() error = %v", err)
	}
}

func TestBuildOutputConcurrentWrites(t *testing.T) {
	// during a step, os/exec drains a command's stdout and stderr
	// on two separate goroutines, so both facets are written
	// concurrently while the callback concurrently reads the
	// step's pipe; this test mirrors that and exists primarily
	// for the race detector
	var total atomic.Int64
	out := newBuildOutput(func(e *StepEvent) error {
		n, err := io.Copy(io.Discard, e.Output)
		total.Add(n)
		return err
	})

	for _, step := range []Step{StepCreateEnvironment, StepCompile} {
		if err := out.beginStep(step); err != nil {
			t.Fatalf("beginStep(%s) error = %v", step, err)
		}
		var writers sync.WaitGroup
		for _, w := range []io.Writer{out.stdout(), out.stderr()} {
			writers.Add(1)
			go func(w io.Writer) {
				defer writers.Done()
				for j := 0; j < 500; j++ {
					fmt.Fprint(w, "xy")
				}
			}(w)
		}
		// as in a real build, all of the step's writes complete
		// before the step ends
		writers.Wait()
	}
	if err := out.endStep(); err != nil {
		t.Fatalf("endStep() error = %v", err)
	}

	if want := int64(2 * 2 * 500 * 2); total.Load() != want {
		t.Errorf("callbacks read %d bytes, want %d", total.Load(), want)
	}
}

func TestBuildOutputAbort(t *testing.T) {
	// a callback error aborts the build at the end of its step:
	// the next step must not begin (its callback is never called)
	sentinel := errors.New("stop right there")
	var order []Step
	out := newBuildOutput(func(e *StepEvent) error {
		order = append(order, e.Step)
		if _, err := io.ReadAll(e.Output); err != nil {
			return err
		}
		if e.Step == StepInitializeModule {
			return sentinel
		}
		return nil
	})

	if err := out.beginStep(StepCreateEnvironment); err != nil {
		t.Fatalf("beginStep() error = %v", err)
	}
	if err := out.beginStep(StepInitializeModule); err != nil {
		t.Fatalf("beginStep() error = %v", err)
	}
	fmt.Fprint(out.stderr(), "init-module output")

	// joining initialize_module's callback surfaces the abort;
	// pin_versions must never begin
	err := out.beginStep(StepPinVersions)
	if !errors.Is(err, sentinel) {
		t.Errorf("beginStep() error = %v, want errors.Is(err, sentinel)", err)
	}
	if !strings.Contains(fmt.Sprint(err), string(StepInitializeModule)) {
		t.Errorf("error %q does not name the aborting step", err)
	}

	wantOrder := []Step{StepCreateEnvironment, StepInitializeModule}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("step order = %v, want %v", order, wantOrder)
	}
}

// TestBuildStepOutput runs a real (network-dependent) partial build
// and verifies that steps are reported in order and that each step's
// output is readable from its event. It is skipped in -short mode.
func TestBuildStepOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent integration test in -short mode")
	}

	var order []Step
	outputs := make(map[Step]string)

	b := Builder{
		Compile:   Compile{Platform: Platform{OS: "linux", Arch: "amd64"}},
		SkipBuild: true, // exercise environment setup + go get, but skip the slow compile
		OnStep: func(e *StepEvent) error {
			order = append(order, e.Step)
			data, err := io.ReadAll(e.Output)
			if err != nil {
				return err
			}
			outputs[e.Step] = string(data)
			return nil
		},
	}

	// Build joins the final callback before returning, so order and
	// outputs are safe to read immediately afterwards
	err := b.Build(context.Background(), filepath.Join(t.TempDir(), "caddy"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	wantOrder := []Step{StepCreateEnvironment, StepInitializeModule, StepPinVersions, StepCleanup}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("step order = %v, want %v", order, wantOrder)
	}

	wantContained := map[Step]string{
		StepCreateEnvironment: "[INFO] Temporary folder:",
		StepInitializeModule:  "[INFO] Initializing Go module",
		StepPinVersions:       "[INFO] Pinning versions",
		StepCleanup:           "[INFO] Cleaning up temporary folder:",
	}
	for step, want := range wantContained {
		if got := outputs[step]; !strings.Contains(got, want) {
			t.Errorf("step %s output missing %q; got:\n%s", step, want, got)
		}
	}
}

// TestBuildAbort verifies that a callback error aborts a real Build
// with the caller's error at the end of the callback's step. It
// aborts at initialize_module, so only local go commands (go mod
// init) execute and no network access occurs.
func TestBuildAbort(t *testing.T) {
	sentinel := errors.New("user says no")
	var order []Step

	b := Builder{
		Compile:   Compile{Platform: Platform{OS: "linux", Arch: "amd64"}},
		SkipBuild: true,
		OnStep: func(e *StepEvent) error {
			order = append(order, e.Step)
			if e.Step == StepInitializeModule {
				return sentinel
			}
			return nil
		},
	}

	err := b.Build(context.Background(), filepath.Join(t.TempDir(), "caddy"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("Build() error = %v, want errors.Is(err, sentinel)", err)
	}

	// pin_versions (the network step) must never have begun
	wantOrder := []Step{StepCreateEnvironment, StepInitializeModule}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("step order = %v, want %v", order, wantOrder)
	}
}

// TestBuildStreamsLinesLive exercises the live-progress consumer
// pattern against a real Build invocation: the callback scans its
// step's output line by line while the step runs, terminating at
// the EOF delivered when the step ends. It aborts after
// initialize_module, so only local go commands execute and no
// network access occurs.
func TestBuildStreamsLinesLive(t *testing.T) {
	sentinel := errors.New("stop before network")

	type line struct {
		step Step
		text string
	}
	var lines []line

	b := Builder{
		Compile:   Compile{Platform: Platform{OS: "linux", Arch: "amd64"}},
		SkipBuild: true,
		OnStep: func(e *StepEvent) error {
			scanner := bufio.NewScanner(e.Output)
			for scanner.Scan() { // consume live, line by line
				lines = append(lines, line{e.Step, scanner.Text()})
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			if e.Step == StepInitializeModule {
				return sentinel
			}
			return nil
		},
	}

	err := b.Build(context.Background(), filepath.Join(t.TempDir(), "caddy"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("Build() error = %v, want errors.Is(err, sentinel)", err)
	}

	byStep := make(map[Step][]string)
	for _, l := range lines {
		byStep[l.step] = append(byStep[l.step], l.text)
	}

	wantContained := map[Step]string{
		StepCreateEnvironment: "[INFO] Temporary folder:",
		StepInitializeModule:  "[INFO] Initializing Go module",
	}
	for step, want := range wantContained {
		found := false
		for _, l := range byStep[step] {
			if strings.Contains(l, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("step %s: no scanned line contains %q; got %d lines:\n%s",
				step, want, len(byStep[step]), strings.Join(byStep[step], "\n"))
		}
	}
}

func TestBuildOutputCallbacksNeverOverlap(t *testing.T) {
	// documented guarantees: callbacks never overlap, and the next
	// step does not begin until the previous step's callback has
	// returned -- even if the callback lingers after reading EOF
	var active, finished atomic.Int32

	out := newBuildOutput(func(e *StepEvent) error {
		if n := active.Add(1); n != 1 {
			t.Errorf("step %s: %d callbacks active, want 1", e.Step, n)
		}
		defer active.Add(-1)

		if _, err := io.ReadAll(e.Output); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond) // linger past EOF
		finished.Add(1)
		return nil
	})

	steps := []Step{StepCreateEnvironment, StepInitializeModule, StepPinVersions, StepCompile}
	for i, step := range steps {
		if err := out.beginStep(step); err != nil {
			t.Fatalf("beginStep(%s) error = %v", step, err)
		}
		// beginStep must have joined all previous callbacks
		if got := finished.Load(); got != int32(i) {
			t.Errorf("after beginStep(%s): %d callbacks finished, want %d", step, got, i)
		}
		fmt.Fprintf(out.stderr(), "output of %s", step)
	}

	if err := out.endStep(); err != nil {
		t.Fatalf("endStep() error = %v", err)
	}
	// endStep must have joined the final callback
	if got := finished.Load(); got != int32(len(steps)) {
		t.Errorf("after endStep: %d callbacks finished, want %d", got, len(steps))
	}
}

func TestBuildOutputRedaction(t *testing.T) {
	outputs := make(map[Step]string)
	out := newBuildOutput(func(e *StepEvent) error {
		data, err := io.ReadAll(e.Output)
		if err != nil {
			return err
		}
		outputs[e.Step] = string(data)
		return nil
	})
	out.configureRedaction([]string{"hunter2secret"}, true)

	if err := out.beginStep(StepPinVersions); err != nil {
		t.Fatalf("beginStep() error = %v", err)
	}
	// an exact secret straddling two writes must still be caught
	fmt.Fprint(out.stderr(), "token=hunter2se")
	fmt.Fprint(out.stderr(), "cret done\n")
	// built-in credential shapes
	fmt.Fprint(out.stderr(), "fetch https://alice:s3cr3t@proxy.example.com/mod\n")
	fmt.Fprint(out.stderr(), "oauth ghp_0123456789abcdefghijklmnop rejected\n")
	fmt.Fprint(out.stderr(), "key AKIAIOSFODNN7EXAMPLE in env\n")
	fmt.Fprint(out.stderr(), "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\n")
	// unterminated final line must be flushed (masked) at step end
	fmt.Fprint(out.stderr(), "trailing hunter2secret partial")
	if err := out.endStep(); err != nil {
		t.Fatalf("endStep() error = %v", err)
	}

	got := outputs[StepPinVersions]
	for _, leaked := range []string{
		"hunter2secret", "s3cr3t", "alice",
		"ghp_0123456789abcdefghijklmnop",
		"AKIAIOSFODNN7EXAMPLE",
		"MIIEowIBAAKCAQEA", "BEGIN RSA PRIVATE KEY",
	} {
		if strings.Contains(got, leaked) {
			t.Errorf("output leaks %q:\n%s", leaked, got)
		}
	}
	for _, want := range []string{
		"token=[REDACTED] done",
		"fetch https://[REDACTED]@proxy.example.com/mod",
		"oauth [REDACTED] rejected",
		"key [REDACTED] in env",
		"trailing [REDACTED] partial",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestEnvironmentHermeticEnv(t *testing.T) {
	// when Env is set, commands receive exactly that environment
	hermetic := []string{"PATH=/usr/bin", "HOME=/tmp/home"}
	env := environment{env: hermetic, out: newBuildOutput(nil)}
	cmd := env.newCommand(context.Background(), "go", "version")
	if !reflect.DeepEqual(cmd.Env, hermetic) {
		t.Errorf("cmd.Env = %v, want %v", cmd.Env, hermetic)
	}

	// when Env is nil, commands inherit the process environment
	// (exec's behavior for a nil cmd.Env)
	env = environment{out: newBuildOutput(nil)}
	cmd = env.newCommand(context.Background(), "go", "version")
	if cmd.Env != nil {
		t.Errorf("cmd.Env = %v, want nil (inherit)", cmd.Env)
	}
}

// TestBuildHermeticEnv runs a real partial build with a minimal,
// credential-free environment and verifies the go toolchain still
// works within it. It aborts after initialize_module, so only local
// go commands execute and no network access occurs.
func TestBuildHermeticEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a minimal hermetic environment needs platform-specific variables on Windows")
	}
	sentinel := errors.New("far enough")
	outputs := make(map[Step]string)

	b := Builder{
		Compile:   Compile{Platform: Platform{OS: "linux", Arch: "amd64"}},
		SkipBuild: true,
		Env: []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + t.TempDir(), // fresh HOME: no .netrc, no .gitconfig, no caches
		},
		OnStep: func(e *StepEvent) error {
			data, err := io.ReadAll(e.Output)
			if err != nil {
				return err
			}
			outputs[e.Step] = string(data)
			if e.Step == StepInitializeModule {
				return sentinel
			}
			return nil
		},
	}

	err := b.Build(context.Background(), filepath.Join(t.TempDir(), "caddy"))
	// reaching the sentinel proves `go mod init` succeeded inside
	// the hermetic environment
	if !errors.Is(err, sentinel) {
		t.Fatalf("Build() error = %v, want errors.Is(err, sentinel)", err)
	}
	if got := outputs[StepInitializeModule]; !strings.Contains(got, "Initializing Go module") {
		t.Errorf("initialize_module output missing init line:\n%s", got)
	}
}

// TestBuildRedactsSecrets verifies end to end that a configured
// secret never reaches step output, even when it appears in values
// xcaddy logs itself (here: the output file path).
func TestBuildRedactsSecrets(t *testing.T) {
	sentinel := errors.New("far enough")
	secret := "hunter2secret"
	var all strings.Builder

	b := Builder{
		Compile:   Compile{Platform: Platform{OS: "linux", Arch: "amd64"}},
		SkipBuild: true,
		Secrets:   []string{secret},
		OnStep: func(e *StepEvent) error {
			data, err := io.ReadAll(e.Output)
			if err != nil {
				return err
			}
			all.Write(data)
			if e.Step == StepInitializeModule {
				return sentinel
			}
			return nil
		},
	}

	outputFile := filepath.Join(t.TempDir(), secret, "caddy")
	err := b.Build(context.Background(), outputFile)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Build() error = %v, want errors.Is(err, sentinel)", err)
	}

	got := all.String()
	if strings.Contains(got, secret) {
		t.Errorf("step output leaks the secret:\n%s", got)
	}
	if !strings.Contains(got, redactedPlaceholder) {
		t.Errorf("step output contains no redaction placeholder; secret was never encountered?\n%s", got)
	}
}
