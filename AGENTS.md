# Repository Guidelines

## Project Structure

This checkout is a small Go module. `go.mod` defines the module, and `docs/` is reserved for project documentation. No Go packages or tests are present yet. As code is added, keep reusable packages at the repository root or in focused subdirectories, put this single application's CLI entry point and commands directly under `cmd/`, and keep tests beside the code they cover.

## Build, Test, and Development

- `go test ./...` runs tests across all packages.
- `go build ./...` compiles all packages.
- `go vet ./...` checks for common Go mistakes.
- `gofmt -w <file.go>` formats Go source files.
- `go run ./cmd` runs the application CLI after its entry point exists.

The current checkout has no Go source packages, so these package commands become useful when implementation files are added. No Makefile or additional development scripts are present.

## Coding Style

Use `gofmt` and standard Go conventions. Name exported identifiers with `PascalCase`, unexported identifiers with `camelCase`, and Go files with short lowercase names. Keep packages focused, handle errors explicitly, and add context when returning errors across package boundaries. No additional lint configuration is present in this checkout.

## Testing

Use Go's standard `testing` package. Put tests in files ending in `_test.go` beside the implementation and name test functions `TestThing`. Run `go test ./...` before submitting changes. No coverage threshold is documented.

## Commits and Pull Requests

Git history is unavailable in this checkout, so no existing commit convention could be confirmed. Write short, imperative commit subjects, such as `Add health check endpoint`. A pull request should explain the change and its reason, list validation commands and results, and link any related issue. Include screenshots only when a change affects a user interface.

## Security and Configuration

Never commit credentials or local secrets. If a change adds required settings, document their names and purpose and provide safe example values in documentation.

## Rules
1. always creating unit test and create test case propose for me and evidence
2. never reading and writing .env, config.ini or .yaml file. you can reading with .example suffix
3. using ponytail for backend, logical and query and using anti slop for creating or working UI/UX

## Anti-slop pointer

For UI/UX work, load the installed anti-slop core and `antislop-ui` skill and apply them during implementation. Use `docs/PRD.md` as the product and visual direction unless the user provides another brief.
