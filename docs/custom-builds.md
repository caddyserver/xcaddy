# Custom builds


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

## Replacing dependencies

If you need to work on Caddy's dependencies, you can use the `--replace` flag to replace it with a local copy of that dependency (or your fork on github etc if you need):

```
$ xcaddy build some-branch-on-caddy \
    --replace golang.org/x/net=../net
```
