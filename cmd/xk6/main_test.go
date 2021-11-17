package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestSplitWith(t *testing.T) {
	for i, tc := range []struct {
		input         string
		expectModule  string
		expectVersion string
		expectReplace string
		expectErr     bool
	}{
		{
			input:        "module",
			expectModule: "module",
		},
		{
			input:         "module@version",
			expectModule:  "module",
			expectVersion: "version",
		},
		{
			input:         "module@version=replace",
			expectModule:  "module",
			expectVersion: "version",
			expectReplace: "replace",
		},
		{
			input:         "module=replace",
			expectModule:  "module",
			expectReplace: "replace",
		},
		{
			input:         "module@module_version=replace@replace_version",
			expectModule:  "module",
			expectReplace: "replace@replace_version",
			expectVersion: "module_version",
		},
		{
			input:     "=replace",
			expectErr: true,
		},
		{
			input:     "@version",
			expectErr: true,
		},
		{
			input:     "@version=replace",
			expectErr: true,
		},
		{
			input:     "",
			expectErr: true,
		},
	} {
		actualModule, actualVersion, actualReplace, actualErr := splitWith(tc.input)
		if actualModule != tc.expectModule {
			t.Errorf("Test %d: Expected module '%s' but got '%s' (input=%s)",
				i, tc.expectModule, actualModule, tc.input)
		}
		if tc.expectErr {
			if actualErr == nil {
				t.Errorf("Test %d: Expected error but did not get one (input='%s')", i, tc.input)
			}
			continue
		}
		if !tc.expectErr && actualErr != nil {
			t.Errorf("Test %d: Expected no error but got: %s (input='%s')", i, actualErr, tc.input)
		}
		if actualVersion != tc.expectVersion {
			t.Errorf("Test %d: Expected version '%s' but got '%s' (input='%s')",
				i, tc.expectVersion, actualVersion, tc.input)
		}
		if actualReplace != tc.expectReplace {
			t.Errorf("Test %d: Expected module '%s' but got '%s' (input='%s')",
				i, tc.expectReplace, actualReplace, tc.input)
		}
	}
}

func TestNormalizeImportPath(t *testing.T) {
	type (
		args struct {
			currentModule string
			cwd           string
			moduleDir     string
		}
		testCaseType []struct {
			name string
			args args
			want string
		}
	)

	tests := testCaseType{
		{"linux-path", args{
			currentModule: "go.k6.io/xk6",
			cwd:           "/xk6",
			moduleDir:     "/xk6",
		}, "go.k6.io/xk6"},
		{"linux-subpath", args{
			currentModule: "go.k6.io/xk6",
			cwd:           "/xk6/subdir",
			moduleDir:     "/xk6",
		}, "go.k6.io/xk6/subdir"},
	}
	windowsTests := testCaseType{
		{"windows-path", args{
			currentModule: "go.k6.io/xk6",
			cwd:           "c:\\xk6",
			moduleDir:     "c:\\xk6",
		}, "go.k6.io/xk6"},
		{"windows-subpath", args{
			currentModule: "go.k6.io/xk6",
			cwd:           "c:\\xk6\\subdir",
			moduleDir:     "c:\\xk6",
		}, "go.k6.io/xk6/subdir"},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests, windowsTests...)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeImportPath(tt.args.currentModule, tt.args.cwd, tt.args.moduleDir); got != tt.want {
				t.Errorf("normalizeImportPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	t.Run(". expands to current directory", func(t *testing.T) {
		got, err := expandPath(".")
		if got == "." {
			t.Errorf("did not expand path")
		}
		if err != nil {
			t.Errorf("failed to expand path")
		}
	})
	t.Run("~ expands to user's home directory", func(t *testing.T) {
		got, err := expandPath("~")
		if got == "~" {
			t.Errorf("did not expand path")
		}
		if err != nil {
			t.Errorf("failed to expand path")
		}
		switch runtime.GOOS {
		case "linux":
			if !strings.HasPrefix(got, "/home") {
				t.Errorf("did not expand home directory. want=/home/... got=%s", got)
			}
		case "darwin":
			if !strings.HasPrefix(got, "/Users") {
				t.Errorf("did not expand home directory. want=/Users/... got=%s", got)
			}
		case "windows":
			if !strings.HasPrefix(got, "C:\\Users") { // could well be another drive letter, but let's assume C:\\
				t.Errorf("did not expand home directory. want=C:\\Users\\... got=%s", got)
			}
		}
	})
}
