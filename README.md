`xcaddy` - Custom Caddy Builder
===============================

This command line tool and associated Go package makes it easy to make custom builds of the [Caddy Web Server](https://github.com/caddyserver/caddy).

It is used heavily by Caddy plugin developers as well as anyone who wishes to make custom `caddy` binaries (with or without plugins).

Stay updated, be aware of changes, and please submit feedback! Thanks!

## Requirements

- [Go installed](https://golang.org/doc/install)

## Install

You can [download binaries](https://github.com/caddyserver/xcaddy/releases) that are already compiled for your platform from the Release tab. 

You may also build `xcaddy` from source:

```bash
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
```

For Debian, Ubuntu, and Raspbian, an `xcaddy` package is available from our [Cloudsmith repo](https://cloudsmith.io/~caddy/repos/xcaddy/packages/):

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/xcaddy/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-xcaddy-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/xcaddy/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-xcaddy.list
sudo apt update
sudo apt install xcaddy
```

## :warning: Pro tip

If you find yourself fighting xcaddy in relation to your custom or proprietary build or development process, **it might be easier to just build Caddy manually!**

Caddy's [main.go file](https://github.com/caddyserver/caddy/blob/master/cmd/caddy/main.go), the main entry point to the application, has instructions in the comments explaining how to build Caddy essentially the same way xcaddy does it. But when you use the `go` command directly, you have more control over the whole thing and it may save you a lot of trouble.

The manual build procedure is very easy: just copy the main.go into a new folder, initialize a Go module, plug in your plugins (add an `import` for each one) and then run `go build`. Of course, you may wish to customize the go.mod file to your liking (specific dependency versions, replacements, etc).


## Command usage

The `xcaddy` command has two primary uses:

1. Compile custom `caddy` binaries
2. A replacement for `go run` while developing Caddy plugins

The `xcaddy` command will use the latest version of Caddy by default. You can customize this for all invocations by setting the `CADDY_VERSION` environment variable.

As usual with `go` command, the `xcaddy` command will pass the `GOOS`, `GOARCH`, and `GOARM` environment variables through for cross-compilation.

Note that `xcaddy` will ignore the `vendor/` folder with `-mod=readonly`.


### Custom builds

Syntax:

```
$ xcaddy build [<caddy_version>]
    [--output <file>]
    [--with <module[@version][=replacement]>...]
    [--replace <module[@version]=replacement>...]
    [--embed <[alias]:path/to/dir>...]
```

- `<caddy_version>` is the core Caddy version to build; defaults to `CADDY_VERSION` env variable or latest.<br>
  This can be the keyword `latest`, which will use the latest stable tag, or any git ref such as:
  - A tag like `v2.0.1`
  - A branch like `master`
  - A commit like `a58f240d3ecbb59285303746406cab50217f8d24`

- `--output` changes the output file.

- `--with` can be used multiple times to add plugins by specifying the Go module name and optionally its version, similar to `go get`. Module name is required, but specific version and/or local replacement are optional.

- `--replace` is like `--with`, but does not add a blank import to the code; it only writes a replace directive to `go.mod`, which is useful when developing on Caddy's dependencies (ones that are not Caddy modules). Try this if you got an error when using `--with`, like `cannot find module providing package`.

- `--embed` can be used to embed the contents of a directory into the Caddy executable. `--embed` can be passed multiple times with separate source directories. The source directory can be prefixed with a custom alias and a colon `:` to write the embedded files into an aliased subdirectory, which is useful when combined with the `root` directive and sub-directive.

#### Examples

```bash
$ xcaddy build \
    --with github.com/caddyserver/ntlm-transport

$ xcaddy build v2.0.1 \
    --with github.com/caddyserver/ntlm-transport@v0.1.1

$ xcaddy build master \
    --with github.com/caddyserver/ntlm-transport

$ xcaddy build a58f240d3ecbb59285303746406cab50217f8d24 \
    --with github.com/caddyserver/ntlm-transport

$ xcaddy build \
    --with github.com/caddyserver/ntlm-transport=../../my-fork

$ xcaddy build \
    --with github.com/caddyserver/ntlm-transport@v0.1.1=../../my-fork
```

You can even replace Caddy core using the `--with` flag:

```
$ xcaddy build \
    --with github.com/caddyserver/caddy/v2=../../my-caddy-fork
    
$ xcaddy build \
    --with github.com/caddyserver/caddy/v2=github.com/my-user/caddy/v2@some-branch
```

This allows you to hack on Caddy core (and optionally plug in extra modules at the same time!) with relative ease.

---

If `--embed` is used without an alias prefix, the contents of the source directory are written directly into the root directory of the embedded filesystem within the Caddy executable. The contents of multiple unaliased source directories will be merged together:

```sh
$ xcaddy build --embed ./my-files --embed ./my-other-files
$ cat Caddyfile
{
    # You must declare a custom filesystem using the `embedded` module.
    # The first argument to `filesystem` is an arbitrary identifier
    # that will also be passed to `fs` directives.
    filesystem my_embeds embedded
}

localhost {
    # This serves the files or directories that were
    # contained inside of ./my-files and ./my-other-files
	file_server {
		fs my_embeds
	}
}
```

You may also prefix the source directory with a custom alias and colon separator to write the source directory's contents to a separate subdirectory within the `embedded` filesystem:

```sh
$ xcaddy build --embed foo:./sites/foo --embed bar:./sites/bar
$ cat Caddyfile
{
    filesystem my_embeds embedded
}

foo.localhost {
    # This serves the files or directories that were
    # contained inside of ./sites/foo
	root * /foo
	file_server {
		fs my_embeds
	}
}

bar.localhost {
    # This serves the files or directories that were
    # contained inside of ./sites/bar
	root * /bar
	file_server {
		fs my_embeds
	}
}
```

This allows you to serve 2 sites from 2 different embedded directories, which are referenced by aliases, from a single Caddy executable.

---

If you need to work on Caddy's dependencies, you can use the `--replace` flag to replace it with a local copy of that dependency (or your fork on github etc if you need):

```
$ xcaddy build some-branch-on-caddy \
    --replace golang.org/x/net=../net
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

The race detector can be enabled by setting `XCADDY_RACE_DETECTOR=1`. The DWARF debug info can be enabled by setting `XCADDY_DEBUG=1`.


### Getting `xcaddy`'s version

```
$ xcaddy version
```


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
- `XCADDY_DEBUG=1` enables the DWARF debug information in the build.
- `XCADDY_SETCAP=1` will run `sudo setcap cap_net_bind_service=+ep` on the resulting binary. By default, the `sudo` command will be used if it is found; set `XCADDY_SUDO=0` to avoid using `sudo` if necessary.
- `XCADDY_SKIP_BUILD=1` causes xcaddy to not compile the program, it is used in conjunction with build tools such as [GoReleaser](https://goreleaser.com). Implies `XCADDY_SKIP_CLEANUP=1`.
- `XCADDY_SKIP_CLEANUP=1` causes xcaddy to leave build artifacts on disk after exiting.
- `XCADDY_WHICH_GO` sets the go command to use when for example more then 1 version of go is installed.
- `XCADDY_GO_BUILD_FLAGS` overrides default build arguments. Supports Unix-style shell quoting, for example: XCADDY_GO_BUILD_FLAGS="-ldflags '-w -s'". The provided flags are applied to `go` commands: build, clean, get, install, list, run, and test
- `XCADDY_GO_MOD_FLAGS` overrides default `go mod` arguments. Supports Unix-style shell quoting.

---

&copy; 2020 Matthew Holt
