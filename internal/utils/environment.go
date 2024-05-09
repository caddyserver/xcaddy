package utils

import (
	"os"
	"runtime"
)

// GetGo returns the go executable to use depending on what
// is set in the XCADDY_WHICH_GO environment variable.
func GetGo() string {
	g := os.Getenv("XCADDY_WHICH_GO")
	if g == "" {
		return "go"
	}
	return g
}

// GetGOOS returns the compilation target OS
func GetGOOS() string {
	o := os.Getenv("GOOS")
	if o == "" {
		return runtime.GOOS
	}
	return o
}

// GetGOARCH returns the compilation target architecture
func GetGOARCH() string {
	a := os.Getenv("GOARCH")
	if a == "" {
		return runtime.GOARCH
	}
	return a
}
