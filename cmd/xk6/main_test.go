package main

import (
	"runtime"
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
			currentModule: "github.com/k6io/xk6",
			cwd:           "/xk6",
			moduleDir:     "/xk6",
		}, "github.com/k6io/xk6"},
		{"linux-subpath", args{
			currentModule: "github.com/k6io/xk6",
			cwd:           "/xk6/subdir",
			moduleDir:     "/xk6",
		}, "github.com/k6io/xk6/subdir"},
	}
	windowsTests := testCaseType{
		{"windows-path", args{
			currentModule: "github.com/k6io/xk6",
			cwd:           "c:\\xk6",
			moduleDir:     "c:\\xk6",
		}, "github.com/k6io/xk6"},
		{"windows-subpath", args{
			currentModule: "github.com/k6io/xk6",
			cwd:           "c:\\xk6\\subdir",
			moduleDir:     "c:\\xk6",
		}, "github.com/k6io/xk6/subdir"},
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
