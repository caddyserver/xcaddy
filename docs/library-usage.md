# Library usage

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


