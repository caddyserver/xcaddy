package utils

import "os"

// GetGo returns the go executable to use depending on what
// is set in the XCADDY_WHICH_GO environment variable.
func GetGo() string {
	g := os.Getenv("XCADDY_WHICH_GO")
	if g == "" {
		return "go"
	}
	return g
}
