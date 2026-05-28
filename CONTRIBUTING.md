# Contributing

Use [just](https://github.com/casey/just) instead of `make`.

```sh
just lefthook # Install pre-commit and pre-push hooks
```

## Releasing

Push a semver tag to publish cross-platform binaries to GitHub Releases:

```sh
git tag v0.1.0 && git push --tags
```

Local snapshot (builds to `dist/`, no publish):

```sh
just goreleaser
```
