# Contributing

Use [mise](https://mise.jdx.dev) for tool dependencies and tasks.

```sh
mise install      # Install tool dependencies
mise run lefthook # Install pre-commit and pre-push hooks
```

## Releasing

Push a semver tag to publish cross-platform binaries to GitHub Releases:

```sh
git tag v0.1.0 && git push --tags
```

Local snapshot (builds to `dist/`, no publish):

```sh
mise run goreleaser
```
