package xk6

import (
	"encoding/json"
	"os/exec"
)

// Compile contains parameters for compilation.
type Compile struct {
	Platform
	Cgo bool `json:"cgo,omitempty"`
}

// CgoEnabled returns "1" if c.Cgo is true, "0" otherwise.
// This is used for setting the CGO_ENABLED env variable.
func (c Compile) CgoEnabled() string {
	if c.Cgo {
		return "1"
	}
	return "0"
}

// Platform represents a build target.
type Platform struct {
	OS   string `json:"os,omitempty"`
	Arch string `json:"arch,omitempty"`
	ARM  string `json:"arm,omitempty"`
}

// SupportedPlatforms runs `go tool dist list` to make
// a list of possible build targets.
func SupportedPlatforms() ([]Compile, error) {
	out, err := exec.Command("go", "tool", "dist", "list", "-json").Output()
	if err != nil {
		return nil, err
	}
	var dists []dist
	err = json.Unmarshal(out, &dists)
	if err != nil {
		return nil, err
	}

	// translate from the go command's output structure
	// to our own user-facing structure
	var compiles []Compile
	for _, d := range dists {
		comp := d.toCompile()
		if d.GOARCH == "arm" {
			if d.GOOS == "linux" {
				// only linux supports ARMv5; see https://github.com/golang/go/issues/18418
				comp.ARM = "5"
				compiles = append(compiles, comp)
			}
			comp.ARM = "6"
			compiles = append(compiles, comp)
			comp.ARM = "7"
			compiles = append(compiles, comp)
		} else {
			compiles = append(compiles, comp)
		}
	}

	return compiles, nil
}

// dist is the structure that fits the output
// of the `go tool dist list -json` command.
type dist struct {
	GOOS         string `json:"GOOS"`
	GOARCH       string `json:"GOARCH"`
	CgoSupported bool   `json:"CgoSupported"`
}

func (d dist) toCompile() Compile {
	return Compile{
		Platform: Platform{
			OS:   d.GOOS,
			Arch: d.GOARCH,
		},
		Cgo: d.CgoSupported,
	}
}
