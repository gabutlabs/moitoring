# Architecture

This project uses a layered application architecture. Each layer has one responsibility, and dependencies flow inward from application entry points toward business logic and data access.

## Project Layout

```text
frontend/
  package.json             # Vue dependencies and scripts
  vite.config.ts           # Vue and Nuxt UI plugins
  index.html
  src/
    main.ts                 # registers Vue, router, and Nuxt UI plugins
    App.vue                 # root component wrapped in UApp
    assets/main.css         # Tailwind CSS and Nuxt UI imports
    components/             # reusable, single-responsibility UI components
    pages/                  # dashboard and settings views
  public/                   # static files copied by the frontend build
  dist/                     # generated frontend build output
internal/
  web/
    web.go                 # embeds the staged frontend bundle
    dist/                  # generated copy of frontend/dist, required by go build
  config/                  # configuration loading and settings types
  handler/                 # HTTP request and response handling
  services/                # validation and business workflows
  tui/                     # Bubble Tea read-only terminal dashboard
  repository/
    sqlite/                # SQLite connection, migrations, and queries
  models/                  # shared domain and persistence field definitions
cmd/
  main.go                 # starts the Cobra root command
  root.go                 # root command and shared flags
  root_test.go            # root command tests
  serve.go                # serve command, Gin setup, and frontend route wiring
  worker.go               # worker command and worker startup
  tui.go                  # TUI command and monitoring dependency wiring
docs/
  architecture.md
```

`frontend/` is a Vue 3 project using Nuxt UI 4 through Vite. Follow the [official Vue installation guide](https://ui.nuxt.com/docs/getting-started/installation/vue); it configures Nuxt UI as a Vue/Vite plugin and does not require the Nuxt framework. Keep frontend source and dependencies in this directory. The frontend build produces `frontend/dist/`; stage that output in `internal/web/dist/` before compiling Go. The `internal/web` package keeps embedded files beside `web.go`, because `//go:embed` paths are relative to their Go package and cannot reference parent directories. See the [Go embed documentation](https://pkg.go.dev/embed).

Keep application code under `internal/` so other Go modules cannot import it directly. Add focused files or subpackages within these directories as the codebase grows.

## Technology Stack

- Go provides the backend; Cobra provides the `serve`, `worker`, and `tui` CLI commands; Bubble Tea renders the terminal dashboard.
- Gin handles HTTP routing. `cmd/serve.go` creates the Gin engine and wires routes; API handlers live in `internal/handler`.
- Vue 3 with Nuxt UI for Vue provides the frontend. Use the current stable packages when setting up the project and commit its package-manager lockfile so builds use reproducible versions.
- SQLite stores application data in one persistent local database file, configured in `config.ini` and shared by the `serve`, `worker`, and `tui` commands.

## Command-Line Interface

Use Cobra commands to select and start application modes. The root command registers subcommands such as:

```sh
moitoring serve -p 8080
moitoring worker
moitoring tui
```

The `serve` command loads configuration, constructs its dependencies, configures Gin, and starts the HTTP server. The `server.web_ui_enabled` setting controls whether Gin serves the embedded browser UI; API and health routes remain available when it is false. The `worker` command loads configuration and starts background processing. The `tui` command loads the same configuration and database, then renders read-only snapshots through the monitoring query service without requiring the HTTP server. Cobra command functions handle flags and application startup; keep business rules in services.

## Frontend Build and Embedded Serving

The frontend framework owns the browser UI and builds production files into `frontend/dist/`. Before `go build`, copy or stage that build output into `internal/web/dist/`. The Go binary embeds the staged bundle, so deployments can run the binary without a separate frontend server or asset directory. Build pipelines must build and stage the frontend before compiling Go.

During development, Vite runs the Vue app and proxies `/api` requests to Gin. In production, `cmd/serve.go` wires Gin routes to files embedded by `internal/web`: `/` serves `index.html`, `/assets/*` serves built assets, and `/favicon.ico` serves the favicon. Set content types from file extensions. If the frontend uses client-side routes, unknown non-API page requests may fall back to `index.html`; missing assets and `/api/*` requests must keep their normal 404 or API behavior. These routes only serve files and must not access repositories or implement business rules.

## SQLite Initialization

Both `serve` and `worker` open the same SQLite path from `config.ini`. If the file is missing, initialization creates its parent directory, creates the database, and applies the initial schema. If the file exists, reuse it and apply only pending, versioned migrations; never replace or reset existing data. Keep initialization safe when both commands start close together, and fail startup with a clear error if the database cannot be opened or migrated.

Keep transactions short and queries indexed for history lookups. Serialize or retry writes when SQLite reports a lock, and configure a bounded busy timeout. Store the database on persistent local disk for this single-server deployment.

Repository tests should use a temporary database path. Cover first-run file and schema creation, reopening an existing database without losing records, applying migrations without resetting data, and concurrent initialization from the `serve` and `worker` startup paths.

## Layer Responsibilities

### Handler

Handlers translate Gin requests into service inputs and service results into API responses. They may parse request data and map errors to response status codes. They must not access repositories or contain business rules. Keep API request and response types at this boundary. The Gin server may wire embedded frontend file routes separately from API handlers; those routes only return static content.

### Service

Services validate data, implement business logic, and coordinate repository operations. They must not depend on HTTP handlers or transport response types. Define small repository interfaces where services need data access.

### Repository

Repositories own SQLite access, persistence, migrations, and queries. They must not implement business rules or create HTTP responses. Return models or data-access errors to the service layer.

### Models and Configuration

Models define fields and data shapes. Keep transport-only request and response structures in handlers. `internal/config` loads and exposes application settings, including the SQLite path; commands pass configuration to the components they construct.

## Dependency Rules

The normal request path is `handler -> service -> repository`. Services and repositories may use models. Commands wire configuration and dependencies, then start the selected mode. Do not bypass a layer, create circular imports, or make services depend on transport details.
