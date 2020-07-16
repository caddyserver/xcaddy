`xcaddy` - Custom Caddy Builder
===============================

This command line tool and associated Go package makes it easy to make custom builds of the [Caddy Web Server](https://github.com/caddyserver/caddy).

It is used heavily by Caddy plugin developers as well as anyone who wishes to make custom `caddy` binaries (with or without plugins).

⚠️ Still in development. Supports Caddy 2 only.

Stay updated, be aware of changes, and please submit feedback! Thanks!

## Requirements

- [Go installed](https://golang.org/doc/install)

## Install

You can [download binaries](https://github.com/caddyserver/xcaddy/releases) that are already compiled for your platform, or build `xcaddy` from source:

```bash
$ go get -u github.com/caddyserver/xcaddy/cmd/xcaddy
```


## Command usage

The `xcaddy` command has two primary uses:

1. Compile custom `caddy` binaries
2. A replacement for `go run` while developing Caddy plugins

The `xcaddy` command will use the latest version of Caddy by default. You can customize this for all invocations by setting the `CADDY_VERSION` environment variable.

As usual with `go` command, the `xcaddy` command will pass the `GOOS`, `GOARCH`, and `GOARM` environment variables through for cross-compilation.


### Custom builds

Syntax:

```
$ xcaddy build [<caddy_version>]
    [--output <file>]
    [--with <module[@version][=replacement]>...]
```

- `<caddy_version>` is the core Caddy version to build; defaults to `CADDY_VERSION` env variable or latest.
- `--output` changes the output file.
- `--with` can be used multiple times to add plugins by specifying the Go module name and optionally its version, similar to `go get`. Module name is required, but specific version and/or local replacement are optional.

Examples:

```bash
$ xcaddy build \
    --with github.com/caddyserver/ntlm-transport

$ xcaddy build v2.0.1 \
    --with github.com/caddyserver/ntlm-transport@v0.1.1

$ xcaddy build \
    --with github.com/caddyserver/ntlm-transport=../../my-fork

$ xcaddy build \
    --with github.com/caddyserver/ntlm-transport@v0.1.1=../../my-fork
```

### For plugin development

If you run `xcaddy` from within the folder of the Caddy plugin you're working on _without the `build` subcommand_, it will build Caddy with your current module and run it, as if you manually plugged it in and invoked `go run`.

The binary will be built and run from the current directory, then cleaned up.

The current working directory must be inside an initialized Go module.

Syntax:

```
$ xcaddy <args...>
```
- `<args...>` are passed through to the `caddy` command.

For example:

```bash
$ xcaddy list-modules
$ xcaddy run
$ xcaddy run --config caddy.json
```

The race detector can be enabled by setting `XCADDY_RACE_DETECTOR=1`.


## Library usage

```go
builder := xcaddy.Builder{
	CaddyVersion: "v2.0.0",
	Plugins: []xcaddy.Dependency{
		{
			ModulePath: "github.com/caddyserver/ntlm-transport",
			Version:    "v0.1.1",
		},
	},
}
err := builder.Build(context.Background(), "./caddy")
```

Versions can be anything compatible with `go get`.



## Environment variables

Because the subcommands and flags are constrained to benefit rapid plugin prototyping, xcaddy does read some environment variables to take cues for its behavior and/or configuration when there is no room for flags.

- `CADDY_VERSION` sets the version of Caddy to build.
- `XCADDY_RACE_DETECTOR=1` enables the Go race detector in the build.
- `XCADDY_SKIP_CLEANUP=1` causes xcaddy to leave build artifacts on disk after exiting.


---

&copy; 2020 Matthew Holt
