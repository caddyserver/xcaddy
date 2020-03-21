Custom Caddy Builder
====================

This package and associated CLI tool make it easy to perform custom builds of the [Caddy Web Server](https://github.com/caddyserver/caddy).

Supports Caddy 2 and up.

## Requirements

- Go installed
- Go modules enabled


## Library usage

```go
caddyVersion := "v2.0.0-beta.19"
plugins := []builder.CaddyPlugin{
	ModulePath: "github.com/caddyserver/nginx-adapter",
	Version:    "6c484552e630ccac384d2d9c43c9d14c4e8d2e56",
}
output := "./caddy"

err := builder.Build(caddyVersion, plugins, output)
```

Versions can be anything compatible with `go get`.


## CLI usage

Syntax:

```
builder --version <version> [--output <file>] [<plugins...>]
```

Where:

- `--version` is the core Caddy version to build (required).
- `--output` changes the output file.
- `<plugins...>` are extra plugins and their versions, in `go get` module syntax: `module@version`

For example:

```bash
$ builder --version v2.0.0-beta.19 \
	github.com/caddyserver/nginx-adapter@6c484552e630ccac384d2d9c43c9d14c4e8d2e56
```


---

&copy; 2020 Matthew Holt
