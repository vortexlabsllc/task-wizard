[![codecov](https://codecov.io/gh/dkhalife/task-wizard/graph/badge.svg?token=UQ4DTE3WI1)](https://codecov.io/gh/dkhalife/task-wizard) [![CodeQL](https://github.com/dkhalife/task-wizard/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/dkhalife/task-wizard/actions/workflows/github-code-scanning/codeql) [![Dependabot Updates](https://github.com/dkhalife/task-wizard/actions/workflows/dependabot/dependabot-updates/badge.svg)](https://github.com/dkhalife/task-wizard/actions/workflows/dependabot/dependabot-updates)

# Task Wizard

**Privacy First, Productivity Always!**

Task Wizard is a free and open-source app designed to help manage tasks effectively. Its primary focus is to give users control over data and allow them to build integrations around it however they choose to.

This repo started as a fork of [DoneTick](https://github.com/donetick/donetick) but has since diverged from the original source code in order to accomplish different goals. Kudos to the contributors of [DoneTick](https://github.com/donetick/donetick) for helping kickstart this project.

## 🎯 Goals and principles

Task Wizard's primary goal is to allow users to own and protect their data and the following principles are ways to accomplish that:

* **Zero PII storage** — the server never stores names, emails, or any personally identifiable information. Authentication is handled entirely by Microsoft Entra ID; the backend only persists an opaque directory/object ID pair to associate tasks with a user
* All the user data sent by this frontend only ever goes to a single backend
* 🔜 When data is stored, it is encrypted with a user key
* The code is continuously scanned by a CI that runs CodeQL
* Dependencies are kept to a minimum
* When vulnerabilities are detected in dependencies they are auto updated with Dependabot

## ✨ Features

✅ Fast and simple task creation and completion for those times you are in a hurry

🏷️ Label assignment to help you categorize and recall tasks efficiently

📅 Due and completion dates tracking for users who need historical records

🔁 Recurring patterns for those chores you don't want to forget

📧 Notifications for important deadlines you don't want to miss

📱 Native Android app with real-time sync

## ⌨️ Keyboard Shortcuts

| Context/Screen                | Shortcut                           | Action or Result                                                   |
|-------------------------------|------------------------------------|-------------------------------------------------------------------|
| Tasks Overview                | `Ctrl + F`                         | Focuses the search box.                                           |
| Tasks Overview                | `+` (outside of inputs)            | Opens the “Add Task” screen.                                      |
| Task Edit and Date modals     | `Enter` in text or date fields     | Submits or saves the form or dialog.                              |

## 🔐 Authentication

Task Wizard uses [Microsoft Entra ID](https://www.microsoft.com/en-us/security/business/identity-access/microsoft-entra-id) (Azure AD) for user authentication. Users sign in via a popup flow using MSAL, and the backend verifies tokens using OIDC.

To set up authentication:

1. Register an application in your Azure AD tenant
2. Configure `entra.tenant_id`, `entra.client_id`, and `entra.audience` in `config.yaml` (or via `TW_ENTRA_*` environment variables)
3. Set `entra.enabled` to `true`

For development without Azure AD, set `entra.enabled` to `false` to enable dev bypass mode (all requests are treated as authenticated).

## 🚀 Installation

### 🚢 Using Docker Compose (recommended)

1. In a compose.yml file, paste the following:

```yaml
services:
   tasks:
      image: dkhalife/task-wizard
      container_name: tasks
      restart: unless-stopped
      ports:
      - 2021:2021
      volumes:
      - /path/to/host/config:/config
```

2. Run the app with `docker compose up -d` 

Alternatively, you can use a `.env` file and reference it in the compose file using an `env_file` entry.

### 🛳️ Using Docker

1. Pull the latest image: `docker pull dkhalife/task-wizard`
1. Run the container:

```bash
docker run \
   -v /path/to/host/config:/config
   -p 2021:2021 \
   dkhalife/task-wizard
```

Make sure to replace `/path/to/host` with your preferred root directory for config.

## ⚙️ Configuration

In the [config](./apiserver/config/) directory are a couple of starter configuration files for prod and dev environments. The server expects a config.yaml in the config directory and will load settings from it when started.

**Note:** You can set Entra ID settings and database credentials using environment variables for improved security and flexibility.

### Telemetry (Application Insights)

Task Wizard supports optional Application Insights telemetry for both the API server and Android app. All events are sent as CustomEvents and include build number, commit hash, and component identifiers.

**API Server:** Set the `APPINSIGHTS_CONNECTION_STRING` environment variable. When not set, telemetry is silently disabled.

**Android App:** Telemetry is **disabled by default**. Users can opt in via Settings → Analytics. When disabled, the app sends a `DNT: 1` header on all API requests, which the backend respects by skipping request telemetry for that user. An additional "Debug logging" sub-toggle sends more detailed diagnostic data when enabled.

### Database Configuration

Task Wizard supports **PostgreSQL** (first-class, recommended for cloud-native
setups such as Azure Database for PostgreSQL, RDS, or Cloud SQL) and **SQLite**
(single-file, local development, zero-config). Connection settings are expressed
as full DSN connection strings, so SSL options (`sslmode`), proxies, and managed
database endpoints all work out of the box.

#### PostgreSQL

Configure a full DSN for the primary (read-write) connection. `dsn` is required
when `database.type` is `postgres` and also acts as the fallback for the other
pools:

```yaml
database:
  type: postgres
  # Primary read-write connection (required for postgres)
  dsn: "postgres://taskuser:taskpass@localhost:5432/taskwizard?sslmode=require"
  # Optional: ordinary reads are routed here when set
  dsn_r: "postgres://ro_user:pass@replica1:5432/taskwizard?sslmode=require"
  # Optional: heavy/reporting reads are routed here when set
  dsn_ro: "postgres://report_user:pass@replica2:5432/taskwizard?sslmode=require"
  migration: true
```

You can also use environment variables (secret-injection friendly):

- `TW_DATABASE_TYPE` - Database type (`postgres` or `sqlite`)
- `TW_DATABASE_DSN` - Primary (read-write) DSN
- `TW_DATABASE_DSN_R` - Read (R) DSN
- `TW_DATABASE_DSN_RO` - Read-only replica (RO) DSN

#### Read / read-only replica routing

The API server opens up to three connection pools and routes workloads by
expected staleness tolerance:

- **`dsn` (RW / primary)** — all writes, transactions, migrations, and
  read-your-writes paths (e.g. session validation, user creation).
- **`dsn_r` (R)** — ordinary reads. Falls back to the primary pool when unset.
- **`dsn_ro` (RO)** — heavy/reporting reads (title search, recent-activity
  joins, scheduled scans). Falls back to the R pool (which may itself be the
  primary) when unset.

> **Replication-lag caveat:** R/RO pools are for reads that can tolerate some
> staleness. Anything user-facing that must observe a write from the same
> request (e.g. "create task, then list tasks") stays on the primary pool to
> avoid read-your-write bugs.

#### SQLite (local development)

SQLite is the zero-config default for local development. It uses a single
file and does not apply an R/RO split:

```yaml
database:
  type: sqlite
  path: /config/task-wizard.db
  migration: true
```

### Authentication Configuration

Configure Entra ID authentication with environment variables or `config.yaml`:

- `TW_ENTRA_ENABLED` - Enable Entra ID authentication (true/false)
- `TW_ENTRA_TENANT_ID` - Azure AD tenant ID
- `TW_ENTRA_CLIENT_ID` - Azure AD application (client) ID
- `TW_ENTRA_AUDIENCE` - Expected token audience

### Configuration Reference

The configuration files are yaml mappings with the following values:

| Configuration Entry                      | Default Value                                       | Description                                                                 |
|------------------------------------------|-----------------------------------------------------|-----------------------------------------------------------------------------|
| `name`                                   | `"prod"`                                            | The name of the environment configuration.                                  |
| `database.type`                          | `sqlite`                                            | Database type: `postgres` or `sqlite`.                                       |
| `database.migration`                     | `true`                                              | Indicates if database migration should be performed.                        |
| `database.path`                          | `/config/task-wizard.db`                            | The path at which to store the SQLite database (SQLite only).               |
| `database.dsn`                           | (empty)                                             | Primary read-write DSN (PostgreSQL; required when `type: postgres`).         |
| `database.dsn_r`                         | (empty)                                             | Read (R) DSN for ordinary reads; falls back to `dsn`.                        |
| `database.dsn_ro`                        | (empty)                                             | Read-only replica (RO) DSN for heavy/reporting reads; falls back to `dsn_r`. |
| `entra.enabled`                          | `false`                                             | Enables Microsoft Entra ID (Azure AD) authentication.                       |
| `entra.tenant_id`                        | (empty)                                             | The Azure AD tenant ID for authentication.                                  |
| `entra.client_id`                        | (empty)                                             | The Azure AD application (client) ID.                                       |
| `entra.audience`                         | (empty)                                             | The expected audience for Entra ID tokens.                                  |
| `server.host_name`                       | `localhost`                                         | The hostname to use for external links.                                     |
| `server.port`                            | `2021`                                              | The port on which the server listens.                                       |
| `server.read_timeout`                    | `2s`                                                | The maximum duration for reading the entire request.                        |
| `server.write_timeout`                   | `1s`                                                | The maximum duration before timing out writes of the response.              |
| `server.rate_period`                     | `60s`                                               | The period for rate limiting.                                               |
| `server.rate_limit`                      | `300`                                               | The maximum number of requests allowed within the rate period.              |
| `server.serve_frontend`                  | `true`                                              | Indicates if the frontend should be served by the backend server.           |
| `server.registration`                    | `true`                                              | Indicates whether new accounts can be created when users first sign in.     |
| `server.log_level`                       | `debug` when `server.debug` = `true`, else `warn`   | The min level to log (debug, info, warn, error, dpanic, panic, fatal).      |
| `server.allowed_origins`                 | `(empty)`                                           | Origins allowed to issue cross-domain requests.                             |
| `server.allow_credentials`               | `false`                                             | Whether cross-domain requests can include credentials.                      |
| `server.trusted_proxies`                 | `(empty)`                                           | CIDRs/IPs of reverse proxies allowed to set `X-Forwarded-*` headers. Empty trusts no proxy and uses the direct peer address. |
| `scheduler_jobs.due_frequency`           | `5m`                                                | The interval for sending regular notifications.                             |
| `scheduler_jobs.overdue_frequency`       | `24h`                                               | The interval for sending overdue notifications.                             |
| `scheduler_jobs.notification_cleanup`    | `10m`                                               | The interval for cleaning up sent notifications.                            |

### Telemetry Configuration

| Environment Variable               | Default Value | Description                                                                 |
|-------------------------------------|---------------|-----------------------------------------------------------------------------|
| `APPINSIGHTS_CONNECTION_STRING`     | (empty)       | Azure Application Insights connection string. When empty, telemetry is disabled. |


## 🛠️ Development

A [devcontainer](./.devcontainer/devcontainer.json) configuration is set up in this repo to help jumpstart development with all the required dependencies available for both the frontend and backend. You can use this configuration alongside
GitHub codespaces to jump into a remote development environment without installing anything on your local machine. For the best experience make sure your codespace has both repos cloned in it. Ports can be forwarded from within the container so that you are able to test changes locally through the VS Code tunnel.

### 📃 Requirements

* [GoLang](https://go.dev)
* [NodeJS](https://nodejs.org) 20+
* [yarn](https://yarnpkg.com)
* [Android Studio](https://developer.android.com/studio) (for Android development)

### 📱 Android App

The Android app lives in the `android/` directory:

```bash
cd android
./gradlew assembleDebug
```

## 🤝 Contributing

Contributions are welcome! If you would like to contribute to this repo, feel free to fork the repo and submit pull requests.
If you have ideas but aren't familiar with code, you can also [open issues](https://github.com/dkhalife/task-wizard/issues).

## 🔒 License

See the [LICENSE](LICENSE) file for more details.
