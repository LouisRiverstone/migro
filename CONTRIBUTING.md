# Contributing to migro

Thanks for helping. The full guide, with setup, how to run tests against a real database, code guidelines and recipes for adding a dialect or a command, is here:

**https://louisriverstone.github.io/migro/contributing/** (source: [docs/contributing.md](docs/contributing.md))

## The short version

```
git clone https://github.com/<you>/migro.git && cd migro
git checkout -b feat/my-change

gofmt -l .                  # must print nothing
go vet ./...
go test -race -count=1 ./...
```

Optional: `pre-commit install` runs `gofmt` and `golangci-lint` (on changed code) before each commit (needs [pre-commit](https://pre-commit.com), `gofmt` and `golangci-lint` installed locally).

1. For anything bigger than a small fix, **open an issue first**.
2. Branch from `main`; keep the PR to one concern.
3. Write tests that fail without your change (SQLite is enough; see the guide for PostgreSQL/MySQL).
4. Update the docs in `docs/` when user-visible behavior changes.
5. Use Conventional Commit messages (`feat:`, `fix:`, `test:`, `docs:`, `refactor:`, `chore:`).
6. Open the PR against `main` and fill in the template. CI must pass.

Security problems: do not open a public issue. Use the **Security** tab → *Report a vulnerability*.

By contributing you agree your work is released under the [MIT license](LICENSE).
