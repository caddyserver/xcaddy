`xk6` - Custom k6 Builder
===============================

This command line tool and associated Go package makes it easy to make custom builds of [k6](https://github.com/k6io/k6).

It is used heavily by k6 extension developers as well as anyone who wishes to make custom `k6` binaries (with or without extensions).

⚠️ Still in development.

Stay updated, be aware of changes, and please submit feedback! Thanks!

## Requirements

- [Go installed](https://golang.org/doc/install)

## Install

You can [download binaries](https://github.com/k6io/xk6/releases) that are already compiled for your platform, or build `xk6` from source:

```bash
$ go install github.com/k6io/xk6/cmd/xk6@latest
```


## Command usage

The `xk6` command has two primary uses:

1. Compile custom `k6` binaries
2. A replacement for `go run` while developing k6 extensions

The `xk6` command will use the latest version of k6 by default. You can customize this for all invocations by setting the `K6_VERSION` environment variable.

As usual with `go` command, the `xk6` command will pass the `GOOS`, `GOARCH`, and `GOARM` environment variables through for cross-compilation.


### Custom builds

Syntax:

```
$ xk6 build [<k6_version>]
    [--output <file>]
    [--with <module[@version][=replacement]>...]
```

- `<k6_version>` is the core k6 version to build; defaults to `K6_VERSION` env variable or latest.
- `--output` changes the output file.
- `--with` can be used multiple times to add extensions by specifying the Go module name and optionally its version, similar to `go get`. Module name is required, but specific version and/or local replacement are optional.

Examples:

```bash
$ xk6 build \
    --with github.com/k6io/xk6-sql

$ xk6 build v0.29.0 \
    --with github.com/k6io/xk6-sql@v0.0.1

$ xk6 build \
    --with github.com/k6io/xk6-sql=../../my-fork

$ xk6 build \
    --with github.com/k6io/xk6-sql=.

$ xk6 build \
    --with github.com/k6io/xk6-sql@v0.0.1=../../my-fork

# Build using a k6 fork repository. Note that a version is required if
# XK6_K6_REPO is a URI.
$ XK6_K6_REPO=github.com/example/k6 xk6 build master \
    --with github.com/k6io/xk6-sql

# Build using a k6 fork repository from a local path. The version must be omitted
# and the path must be absolute.
$ XK6_K6_REPO="$PWD/../../k6" xk6 build \
    --with github.com/k6io/xk6-sql
```

### For extension development

If you run `xk6` from within the folder of the k6 extension you're working on _without the `build` subcommand_, it will build k6 with your current module and run it, as if you manually plugged it in and invoked `go run`.

The binary will be built and run from the current directory, then cleaned up.

The current working directory must be inside an initialized Go module.

Syntax:

```
$ xk6 <args...>
```
- `<args...>` are passed through to the `k6` command.

For example:

```bash
$ xk6 version
$ xk6 run -u 10 -d 10s test.js
```

The race detector can be enabled by setting `XK6_RACE_DETECTOR=1`.


## Library usage

```go
builder := xk6.Builder{
	k6Version: "v0.29.0",
	Extensions: []xk6.Dependency{
		{
			ModulePath: "github.com/k6io/xk6-sql",
			Version:    "v0.0.1",
		},
	},
}
err := builder.Build(context.Background(), "./k6")
```

Versions can be anything compatible with `go get`.



## Environment variables

Because the subcommands and flags are constrained to benefit rapid extension prototyping, xk6 does read some environment variables to take cues for its behavior and/or configuration when there is no room for flags.

- `K6_VERSION` sets the version of k6 to build.
- `XK6_RACE_DETECTOR=1` enables the Go race detector in the build.
- `XK6_SKIP_CLEANUP=1` causes xk6 to leave build artifacts on disk after exiting.
- `XK6_K6_REPO` optionally sets the path to the main k6 repository. This is useful when building with k6 forks.


---

&copy; 2020 Matthew Holt
