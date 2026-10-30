# Product Requirements Document: Moitoring

**Status:** Draft  
**Release:** MVP, date TBD  
**Product:** Self-hosted web application with background collection workers

## 1. Summary

Moitoring gives operators one place to see whether selected systemd worker services are running as expected and whether RabbitMQ queues are accumulating work or losing consumers. A web dashboard presents current state and recent trends. A background worker collects observations without changing the monitored systems.

This PRD defines product behavior and MVP scope. Implementation should follow the project's [layered architecture](architecture.md). Database analysis with `pg_activity` is a later product area and is not part of this release.

## 2. Problem and Opportunity

When a worker stops or a queue backs up, operators often have to inspect systemd and RabbitMQ separately. They need a quick, trustworthy view of the affected service or queue, how recent the data is, and whether the issue is still active. Moitoring should reduce that investigation work while remaining read-only and clear when a data source cannot be reached.

## 3. Users and Jobs to Be Done

- **On-call operator:** When a job appears delayed, identify the affected queue or worker and its current state without switching between tools.
- **Service owner:** When deploying or debugging a worker, confirm that its systemd service is active and that its queue has consumers and is draining.
- **Platform administrator:** Configure monitored sources and targets with least-privilege credentials, then understand when collection has stopped.
- **On-call operator:** Receive an email when a configured worker fails or a RabbitMQ queue breaches its configured health conditions.

## 4. Goals and Non-goals

### Goals

- Show current state and data freshness for configured systemd worker services and RabbitMQ queues.
- Help an operator distinguish a healthy target from a failed, unexpectedly inactive, backlogged, or stale target.
- Keep monitoring read-only. The MVP must not start, stop, restart, acknowledge, publish, purge, or otherwise modify monitored resources.
- Provide enough recent history to see whether queue depth is rising or falling.
- Email the configured recipients when a monitored worker or RabbitMQ queue needs attention.

### Non-goals for MVP

- PostgreSQL analysis through `pg_activity`.
- Log search, tracing, worker deployment, queue administration, or remediation actions.
- Notification delivery to chat or paging systems.
- A general-purpose metrics platform or arbitrary dashboard builder.

## 5. MVP Assumptions and Scope

Planning assumptions to validate in a pilot:

1. The MVP deploys one Moitoring application instance on one Linux monitoring server. That instance monitors local systemd services directly, remote Linux services through SSH using a restricted, read-only account, and configured RabbitMQ sources.
2. The current RabbitMQ test broker is version 3.10.2. Moitoring discovers queues in configured virtual hosts and lets the operator monitor all of them or exclude selected queues. For systemd, it discovers services on configured hosts and lets the operator select the worker services; it does not assume every `.service` unit is a worker.
3. Queue history retention is configurable in `config.ini`, with a proposed default of 24 hours. Operators may set a longer period, subject to available storage.
4. Queue message thresholds have no implicit numeric defaults: the operator must configure a threshold for each queue before threshold-based email alerts are active. The dashboard identifies queues whose alert thresholds are not configured. Consumer-loss alerts are active only when the operator configures an expected consumer count. Expected systemd state is configured per service.
5. SMTP settings, global alert recipients, reminder interval, history retention, SQLite file path, and the password-change flag are supplied through `config.ini`.
6. The MVP supports one administrator account. Its static email identity comes from `config.ini`; first-run setup creates the account once and the administrator sets a password in the frontend. Sign-in asks only for the password. The admin email is separate from SMTP sender and alert-recipient settings.

### Application Stack and Data Initialization

The backend uses Go with Gin for HTTP routing and Cobra for the `serve` and `worker` commands. The frontend is a Vue 3 application using Nuxt UI for Vue with Vite; follow the [official Vue installation guide](https://ui3.nuxt.com/getting-started/installation/vue). This is the Vue integration of Nuxt UI and does not require the Nuxt framework. SQLite is the application database. The `serve` and `worker` commands use the same database path from `config.ini` on the monitoring server.

On first startup, Moitoring creates the SQLite file and initializes its schema. On later startups, it opens the existing database and preserves its data, applying only pending versioned migrations. It must not overwrite or reset an existing database.

### RabbitMQ Connector Choice

Use RabbitMQ's Management HTTP API as the MVP connector for queue discovery and per-queue snapshots. It requires the `rabbitmq_management` plugin and supports authenticated, read-only access; the HTTP API listens on port 15672 by default. RabbitMQ 3.10.2 also includes the native `rabbitmq_prometheus` exporter, introduced in 3.8. Prometheus is a metrics format and collector ecosystem: Moitoring can request RabbitMQ's Prometheus-compatible HTTP endpoint directly, without installing a separate Prometheus server. The plugin must be enabled and its endpoint reachable from Moitoring (port 15692 by default). Keep it as an optional integration until the pilot shows that it provides metrics the API does not. See the [Management API guide](https://www.rabbitmq.com/docs/3.13/management), [Prometheus guide](https://www.rabbitmq.com/docs/3.13/prometheus), and [RabbitMQ 3.8 release notes](https://www.rabbitmq.com/blog/2019/11/11/rabbitmq-3-8-release-overview).

Use `rabbitmq-diagnostics` for manual broker and node health checks, and `rabbitmqctl list_queues` for one-off queue inspection. Do not make Moitoring shell out to these commands for routine collection: RabbitMQ CLI access depends on Erlang distribution connectivity and a shared Erlang cookie. The authenticated Management HTTP API is a better fit for Moitoring's recurring remote queue polling. See the [RabbitMQ CLI documentation](https://www.rabbitmq.com/docs/3.13/cli) and [HTTP API reference](https://www.rabbitmq.com/docs/4.1/http-api-reference).

MVP includes a web dashboard, local and remote source configuration, discovery of queues and systemd services, a collector process, current status, freshness indicators, configurable queue-history retention, visual warning states, and email alerts. The `serve` command starts the web application; `worker` starts collection. Example invocation:

```sh
moitoring serve -p 8080
moitoring worker
```

## 6. User Experience

1. An operator configures a RabbitMQ source and selects virtual hosts and queues, then selects the systemd worker units on the monitored host.
2. The operator opens the dashboard and sees source connectivity, each target's status, and when each observation was last collected.
3. The operator opens a queue or service to inspect its state and recent queue-depth trend.
4. If a source becomes unavailable, Moitoring marks its observations stale and shows the collection error. It does not present the last successful value as current.
5. When a configured alert condition starts, the operator receives an email. When the condition clears, Moitoring sends a recovery email.

## 7. Functional Requirements

| ID | Priority | Requirement and acceptance criteria |
| --- | --- | --- |
| CFG-1 | Must | An operator can configure RabbitMQ connection details, virtual-host scope, selected queues, and credentials. Secrets are write-only in the UI and are never shown in logs or error messages. |
| CFG-2 | Must | An operator can select systemd `.service` units and define the expected state for each; the default expected state for a selected worker is `active`. Only configured units are shown as monitored worker targets. |
| SYS-1 | Must | For every configured unit, the dashboard shows the unit name, raw `ActiveState`, `SubState`, latest result when available, expected state, and observation time. A unit expected to be `active` is unhealthy when `ActiveState` is `inactive`, `failed`, or any other non-expected value. Unknown or newly introduced state values remain visible rather than being silently mapped to healthy. |
| RAB-1 | Must | For each configured queue, show total messages, ready messages, unacknowledged messages, consumer count, and ingress/egress rates when the selected RabbitMQ integration exposes them. Missing metrics display as unavailable, not zero. |
| RAB-2 | Must | Discover queues in each configured virtual host and allow the operator to monitor all discovered queues, with an exclusion option for temporary or irrelevant queues. Do not impose a fixed 100-target product cap. |
| COL-1 | Must | The background worker records the last successful observation and last collection error for each source. After two missed collection intervals, affected data is marked stale and is visually distinct from a healthy current observation. |
| UI-1 | Must | The dashboard groups targets by source and clearly distinguishes expected, warning, failed, stale, and unconfigured alert states. Queue thresholds and expected consumer counts, when set, and expected systemd states are visible and editable by an authorized operator. |
| HIST-1 | Must | Queue detail shows history for the duration configured by `history_retention` in `config.ini`, with a proposed default of 24 hours. Gaps caused by failed collection are shown as gaps, not interpolated as zero. |
| DATA-1 | Must | On first startup, create the SQLite database file and initial schema at the configured path. On subsequent starts, reuse the existing database and preserve records; never reset or replace it. |
| DATA-2 | Must | Apply versioned schema migrations without losing stored data. The `serve` and `worker` commands use the same database path, and startup reports a clear error if database initialization or migration fails. |
| ALERT-1 | Must | Send an email to the global recipients when a monitored systemd worker differs from its configured expected state (default `active`), or when a RabbitMQ queue crosses its configured message threshold, falls below its configured expected consumer count, or becomes unavailable. Never infer an any-backlog threshold; a queue without a configured message threshold is marked unconfigured for threshold-based alerts. Treat RabbitMQ connection failure as a source alert. |
| ALERT-2 | Must | Read SMTP server, port, sender, global recipients, authentication, TLS, and unresolved-incident reminder interval from `config.ini`. Never display or log credentials. Validate required settings at startup and report actionable configuration errors. |
| ALERT-3 | Must | Send one email when an incident begins and a recovery email whenever it clears. Send reminders for unresolved incidents at the interval configured in `config.ini`. Repeated collection cycles must not send messages before the reminder interval. |
| ALERT-4 | Should | If email delivery fails, retry with bounded backoff, keep the alert visible in the dashboard, and expose the delivery failure for diagnosis. |
| AUTH-1 | Must | Require authentication before showing monitoring data. Use the static administrator email from `config.ini` as the account identity and create exactly one administrator record during first-run setup. Do not create duplicates on restart. |
| AUTH-2 | Must | The single-administrator sign-in screen accepts a password only. Do not enable public registration or multiple accounts in MVP. |
| AUTH-3 | Must | Store passwords as salted adaptive password hashes, never as plaintext or reversible encryption. Rate-limit login attempts and define session expiration and logout behavior. |
| AUTH-4 | Must | When `is_changed_password=true` in `config.ini`, the next page load shows a password-change form, never the existing or new password. Limit this recovery flow to an authorized reset path, then clear the flag after a successful change. |
| SYS-REMOTE-1 | Must | Discover systemd services on configured local and remote Linux hosts. Inspect remote hosts through SSH using a restricted, read-only account. An operator can select all worker services or a subset. Show each host's connection status and mark its service observations stale when remote collection fails. |
| CLI-1 | Must | `moitoring serve -p <port>` starts the web process on the requested port. `moitoring worker` starts collection. Invalid or incomplete configuration produces an actionable error and a non-zero exit status. |
| SAFE-1 | Must | The application only reads monitoring data. The UI and collector expose no resource-changing operations for systemd or RabbitMQ. |

## 8. Non-functional Requirements

- **Freshness:** The background worker polls RabbitMQ queues and configured systemd units every 60 seconds. When a source is reachable, dashboard observations should normally be no older than two intervals. Validate the interval against representative RabbitMQ queue counts during the pilot.
- **Trust:** Every displayed value includes an observation time or an unavailable/stale state. A source outage must not be mistaken for a healthy target.
- **Security:** Use the one-time administrator bootstrap and authenticated sessions described above. Use least-privilege read access for RabbitMQ and systemd; restrict remote SSH access to inspection commands and verify host identities. Protect source credentials and SSH keys, along with SMTP settings in `config.ini`; restrict access to secret files, use TLS for SMTP and network connections where supported, and redact secrets from logs.
- **Email delivery:** When the SMTP service is reachable, deliver an alert within 60 seconds of detecting its condition. Track failed delivery separately from the monitored system's health.
- **Performance:** Bound collection concurrency and request timeouts. Collect only fields needed by the MVP and paginate large inventories. Do not impose an arbitrary 100-target limit; use the customer's actual queue and worker counts to benchmark capacity and set deployment guidance.
- **Compatibility:** Preserve raw systemd state values because the set of states can vary across systemd versions. Treat unsupported or missing RabbitMQ metrics as unavailable.

## 9. Success Measures

These are proposed MVP targets, not claims about current performance. Establish a baseline during the pilot and revise targets before release:

- In a task-based pilot, at least 4 of 5 operators identify the affected worker or queue within two minutes without using shell commands.
- When sources are reachable, at least 95% of configured targets have an observation no older than two collection intervals over a seven-day pilot.
- Every simulated source outage is shown as stale within two missed intervals, with no stale value presented as current.
- Every simulated worker or queue alert sends one email within 60 seconds when SMTP is reachable; repeated polls during an incident do not create duplicate messages.
- No systemd or RabbitMQ state-changing operation is performed by the application.

## 10. Risks and Dependencies

- RabbitMQ 3.10.2 is outside the currently supported release lines. Treat it as a compatibility target, validate Moitoring against the actual broker, and plan an upgrade because unsupported releases no longer receive community fixes. See [RabbitMQ release information](https://www.rabbitmq.com/release-information).
- Queue discovery and metrics collection can create broker load, especially at high queue counts. Use the Management API's paginated inventory endpoints, collect only required fields, and benchmark against the actual inventory and polling interval. The built-in exporter is available in 3.10.2, but its endpoint must be enabled and reachable; direct scraping does not require a Prometheus server. See the [Management API guide](https://www.rabbitmq.com/docs/3.13/management) and [Prometheus guide](https://www.rabbitmq.com/docs/3.13/prometheus).
- systemd exposes high-level `ActiveState` and unit-specific `SubState`; state values may vary. The UI must preserve source values and avoid assuming a fixed exhaustive list. See the [systemd D-Bus API](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.systemd1.html).
- Remote systemd monitoring depends on SSH reachability, host identity verification, and sufficient read-only permissions on each host.
- Queue email alerts require operators to configure the relevant message thresholds or expected consumer counts. The UI must make unconfigured alert rules clear so that a missing threshold is not mistaken for a healthy queue.
- Email alert delivery depends on valid SMTP settings, reachable mail infrastructure, and protected credentials in `config.ini`.

## 11. PRD Practice Notes and References

This draft separates the user problem, goals, non-goals, assumptions, requirements, measurable acceptance criteria, and risks. Keep product outcomes in this document; put implementation details and layer boundaries in [architecture.md](architecture.md). Revisit assumptions and success measures with operators during discovery and the pilot. This structure follows guidance to document user needs, goals, success metrics, assumptions, and explicit out-of-scope work in an iterative PRD ([Atlassian PRD guide](https://www.atlassian.com/agile/product-management/requirements/)). For authentication decisions, follow OWASP guidance on [authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) and [password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).
