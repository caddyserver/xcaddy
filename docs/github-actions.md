# Building Caddy with GitHub Actions

An example workflow that builds Caddy with xcaddy and caches Go modules between runs.

```yaml
name: build-caddy
on: push

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: stable
          cache: false

      - uses: actions/cache@v4
        with:
          path: ~/go/pkg/mod
          key: xcaddy-${{ runner.os }}-caddy-2.11.4

      - run: go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

      - run: xcaddy build --with github.com/caddy-dns/cloudflare

      - run: ./caddy version
```

## Why the cache step comes first

The cache step has to run before anything else writes to `~/go/pkg/mod`. `actions/cache` restores with `tar -xf`, which will not overwrite files that are already there. If `go install` runs first, it populates the module cache with xcaddy's own dependencies, and the restore then fails part way through with errors like:

```
/usr/bin/tar: .../go/pkg/mod/...: Cannot open: File exists
```

The log still says `Cache hit`, so this is easy to miss. The build then downloads everything again and the cache saves no time at all.

## Cache key

Caddy's dependencies are determined by the Caddy version and the plugins you build with, not by this repository's `go.sum`. That is also why `cache: false` is set on `actions/setup-go`, whose built-in caching keys off `go.sum` and would not match what `xcaddy build` downloads. Put the versions you build with in the cache key so that changing them fetches a fresh set.

