# Embedding files

If `--embed` is used without an alias prefix, the contents of the source directory are written directly into the root directory of the embedded filesystem within the Caddy executable. The contents of multiple unaliased source directories will be merged together:

```
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

```
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
