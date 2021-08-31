//go:build !windows
// +build !windows

package xcaddycmd

import (
	"reflect"
	"testing"

	"github.com/caddyserver/xcaddy"
)

func TestParseGoListJson(t *testing.T) {
	currentModule, moduleDir, replacements, err := parseGoListJson([]byte(`
{
	"Path": "replacetest1",
	"Version": "v1.2.3",
	"Replace": {
		"Path": "golang.org/x/example",
		"Version": "v0.0.0-20210811190340-787a929d5a0d",
		"Time": "2021-08-11T19:03:40Z",
		"GoMod": "/home/simnalamburt/.go/pkg/mod/cache/download/golang.org/x/example/@v/v0.0.0-20210811190340-787a929d5a0d.mod",
		"GoVersion": "1.15"
	},
	"GoMod": "/home/simnalamburt/.go/pkg/mod/cache/download/golang.org/x/example/@v/v0.0.0-20210811190340-787a929d5a0d.mod",
	"GoVersion": "1.15"
}
{
	"Path": "replacetest2",
	"Version": "v0.0.1",
	"Replace": {
		"Path": "golang.org/x/example",
		"Version": "v0.0.0-20210407023211-09c3a5e06b5d",
		"Time": "2021-04-07T02:32:11Z",
		"GoMod": "/home/simnalamburt/.go/pkg/mod/cache/download/golang.org/x/example/@v/v0.0.0-20210407023211-09c3a5e06b5d.mod",
		"GoVersion": "1.15"
	},
	"GoMod": "/home/simnalamburt/.go/pkg/mod/cache/download/golang.org/x/example/@v/v0.0.0-20210407023211-09c3a5e06b5d.mod",
	"GoVersion": "1.15"
}
{
	"Path": "replacetest3",
	"Version": "v1.2.3",
	"Replace": {
		"Path": "./fork1",
		"Dir": "/home/work/module/fork1",
		"GoMod": "/home/work/module/fork1/go.mod",
		"GoVersion": "1.17"
	},
	"Dir": "/home/work/module/fork1",
	"GoMod": "/home/work/module/fork1/go.mod",
	"GoVersion": "1.17"
}
{
	"Path": "github.com/simnalamburt/module",
	"Main": true,
	"Dir": "/home/work/module",
	"GoMod": "/home/work/module/go.mod",
	"GoVersion": "1.17"
}
{
	"Path": "replacetest4",
	"Version": "v0.0.1",
	"Replace": {
		"Path": "/srv/fork2",
		"Dir": "/home/work/module/fork2",
		"GoMod": "/home/work/module/fork2/go.mod",
		"GoVersion": "1.17"
	},
	"Dir": "/home/work/module/fork2",
	"GoMod": "/home/work/module/fork2/go.mod",
	"GoVersion": "1.17"
}
{
	"Path": "replacetest5",
	"Version": "v1.2.3",
	"Replace": {
		"Path": "./fork3",
		"Dir": "/home/work/module/fork3",
		"GoMod": "/home/work/module/fork3/go.mod",
		"GoVersion": "1.17"
	},
	"Dir": "/home/work/module/fork3",
	"GoMod": "/home/work/module/fork3/go.mod",
	"GoVersion": "1.17"
}
`))
	if err != nil {
		t.Errorf("Error occured during JSON parsing")
	}
	if currentModule != "github.com/simnalamburt/module" {
		t.Errorf("Unexpected module name")
	}
	if moduleDir != "/home/work/module" {
		t.Errorf("Unexpected module path")
	}
	expected := []xcaddy.Replace{
		xcaddy.NewReplace("replacetest1@v1.2.3", "golang.org/x/example@v0.0.0-20210811190340-787a929d5a0d"),
		xcaddy.NewReplace("replacetest2@v0.0.1", "golang.org/x/example@v0.0.0-20210407023211-09c3a5e06b5d"),
		xcaddy.NewReplace("replacetest3@v1.2.3", "/home/work/module/fork1"),
		xcaddy.NewReplace("github.com/simnalamburt/module", "/home/work/module"),
		xcaddy.NewReplace("replacetest4@v0.0.1", "/srv/fork2"),
		xcaddy.NewReplace("replacetest5@v1.2.3", "/home/work/module/fork3"),
	}
	if !reflect.DeepEqual(replacements, expected) {
		t.Errorf("Expected replacements '%v' but got '%v'", expected, replacements)
	}
}
