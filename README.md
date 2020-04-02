Custom Caddy Builder
====================

This package and associated CLI tool make it easy to perform custom builds of the [Caddy Web Server](https://github.com/caddyserver/caddy).

Supports Caddy 2 and up.

⚠️ Still in early development. Works; but no guarantees and prone to changes at this point. Stay updated and please submit feedback!

## Requirements

- Go installed
- Go modules enabled


## Library usage

```go
caddyVersion := "v2.0.0-beta.20"
plugins := []xcaddy.Dependency{
	xcaddy.Dependency{
		ModulePath: "github.com/caddyserver/nginx-adapter",
		Version:    "6c484552e630ccac384d2d9c43c9d14c4e8d2e56",
	},
}
output := "./caddy"

err := xcaddy.Build(caddyVersion, plugins, output)
```

Versions can be anything compatible with `go get`.


## CLI usage

The CLI can be used both to make custom builds of Caddy, but also as a replacement for `go run` while developing Caddy plugins.

### For custom builds

Syntax:

```
xcaddy build <version>
	[--output <file>]
	[--with <module[@version]>...]
```

Where:

- `--version` is the core Caddy version to build (required, for now).
- `--output` changes the output file.
- `--with` can be used multiple times to add plugins by specifying the module name and optionally its version, in a way similar to `go get`.

For example:

```bash
$ xcaddy build v2.0.0-beta.20 \
	--with github.com/caddyserver/nginx-adapter@6c484552e630ccac384d2d9c43c9d14c4e8d2e56
```

### For plugin development

If you run `xcaddy` from within the folder of the Caddy plugin you're working on without the `build` subcommand, it will build Caddy with your current module and run it, similar to if you manually plugged it in and ran `go run`.

The binary will be built, run from the current directory, then cleaned up.

Syntax:

```
xcaddy <args...>
```

Where:

- `<args...>` are passed through to the `caddy` command.

For example:

```bash
$ xcaddy list-modules
$ xcaddy run --config caddy.json
```


---

&copy; 2020 Matthew Holt
