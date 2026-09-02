# For plugin development

If you run `xcaddy` from within the folder of the Caddy plugin you're working on _without the `build` subcommand_, it will build Caddy with your current module and run it, as if you manually plugged it in and invoked `go run`.

The binary will be built and run from the current directory, then cleaned up.

The current working directory must be inside an initialized Go module.

Syntax:

```
$ xcaddy [--] <args...>
```

- `<args...>` are passed through to the `caddy` command. This is **not** the place for xcaddy build flags such as `--with`.
- Use `--` before Caddy flags (such as `--config`) so that `xcaddy` does not try to parse them as its own flags. Without the separator, commands like `xcaddy run --config caddy.json` fail with `unknown flag: --config`.


For example:

```bash
$ xcaddy list-modules
$ xcaddy run
$ xcaddy -- run --config caddy.json
```

#### Building with your plugin plus other plugins

The development shortcut above only includes the module in the current directory. Flags like `--with` belong to `xcaddy build`. If you put them on a bare `xcaddy` / `xcaddy run` invocation, they are passed through to Caddy and fail with `unknown flag: --with`.

To produce a binary that includes your local plugin **and** one or more other plugins, use `xcaddy build` with multiple `--with` flags. Point your own module at the current directory with `=.`:

```bash
# from inside your plugin's module directory
$ xcaddy build \
    --with github.com/me/my-plugin=. \
    --with github.com/mholt/caddy-events-exec

$ ./caddy run
```

You can add as many `--with` plugins as you need. After the build finishes, run the resulting binary (default `./caddy`) yourself — same as any other custom build.

