<div align="center">

<img src="./web/public/favicon.svg" alt="GPT-Load" width="96">

# GPT-Load

**A self-hosted AI gateway for multi-channel, multi-credential setups**

API keys, subscription accounts, traffic scheduling, failure handling, request logs, and usage accounting — behind a single entry point.

English · [中文](README_CN.md) · [日本語](README_JP.md) | [Official Website](https://www.gpt-load.com)

[![Upstream Release](https://img.shields.io/github/v/tag/tbphp/gpt-load?filter=v2.*&label=upstream%20release)](https://github.com/tbphp/gpt-load/releases)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io%2F0401lucky%2Fgpt--load%3Alatest-2496ED?logo=docker&logoColor=white)](https://github.com/0401lucky/gpt-load/pkgs/container/gpt-load)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

<a href="https://trendshift.io/repositories/14880" target="_blank"><img src="https://trendshift.io/api/badge/repositories/14880" alt="tbphp/gpt-load | Trendshift" width="220" height="48"/></a>
<a href="https://hellogithub.com/repository/tbphp/gpt-load" target="_blank"><img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=554dc4c46eb14092b9b0c56f1eb9021c&claim_uid=Qlh8vzrWJ0HCneG" alt="Featured｜HelloGitHub" width="220" height="47"/></a>

</div>

---

## Sponsors

<sub>[Become a sponsor](mailto:tangb7420@gmail.com)</sub>

<details open>
<summary>Sponsor details (collapsible)</summary>

<table>
<tbody>
<tr>
<td width="180"><a href="https://ofox.ai/?utm_source=github&amp;utm_medium=sponsorship&amp;utm_content=gpt_load" target="_blank" rel="noopener noreferrer"><img src="./screenshot/ofoxai.svg" alt="OfoxAI" width="150"></a></td>
<td><strong>OfoxAI: Text, image, and video AI in one platform</strong><br>OfoxAI is a unified AI API platform bringing together text, image, and video models from multiple providers. With OpenAI-compatible endpoints and native Anthropic and Gemini interfaces, developers can access models for AI applications, agents, and content creation through one platform. <a href="https://ofox.ai/?utm_source=github&amp;utm_medium=sponsorship&amp;utm_content=gpt_load" target="_blank" rel="noopener noreferrer">Explore OfoxAI models and APIs →</a></td>
</tr>
<tr>
<td width="180"><a href="https://www.packyapi.ai/register?aff=ahiS" target="_blank" rel="sponsored noopener noreferrer"><picture><source media="(prefers-color-scheme: dark)" srcset="./screenshot/packycode-dark.png"><source media="(prefers-color-scheme: light)" srcset="./screenshot/packycode-light.png"><img src="./screenshot/packycode-light.png" alt="PackyCode" width="150"></picture></a></td>
<td><strong>PackyCode</strong><br>Access leading AI models through PackyCode with one API endpoint and one API key. Enjoy fast, reliable access with automatic failover and dedicated high-speed routes for Codex and Claude Code. Get started with $1 in free credits, a discount on your first top-up, and savings of up to 80% on eligible routes. Pay in RMB with no currency conversion markups or extra top-up fees. <a href="https://www.packyapi.ai/register?aff=ahiS" target="_blank" rel="sponsored noopener noreferrer">Sign up through the link and start building today.</a></td>
</tr>
<tr>
<td width="180"><a href="https://fluxionai.space/register?source=github&amp;campaign=gptload&amp;promo=GPTLOAD" target="_blank" rel="sponsored noopener noreferrer"><img src="./screenshot/fluxionai-horizontal.png" alt="Fluxion AI" width="150"></a></td>
<td><strong>One entry point to connect and manage the world's leading AI models</strong><br>Fluxion AI serves individual developers, technical teams, and enterprises with a unified API for connecting to and managing leading AI models worldwide. Dynamic multi-route scheduling improves availability, while model performance, response times, and costs remain transparent and easy to review. Depending on the model and route, API calls can cost 40%–98% less than official or benchmark prices. Visit and sign up now to receive $7 in API credits. (<a href="https://fluxionai.space/register?source=github&amp;campaign=gptload&amp;promo=GPTLOAD" target="_blank" rel="sponsored noopener noreferrer">Dedicated link</a>)</td>
</tr>
<tr>
<td width="180"><a href="https://www.axisnow.io/zh"><img src="./screenshot/axisnow.jpg" alt="AxisNow" width="150"></a></td>
<td>Protect and accelerate websites and APIs, <strong>serving users in mainland China</strong> and around the world, and extend acceleration and security capabilities to native/mobile apps through a client SDK — <strong>self-built private-deployment CDN | subscription-based high-protection CDN | an independently controllable, flexibly composable CDN network.</strong></td>
</tr>
<tr>
<td width="180"><a href="https://go.apimart.ai/gh-gpt-load"><img src="./screenshot/apimart.png" alt="APIMart" width="150"></a></td>
<td>Thanks to APIMart for sponsoring this project! APIMart is a low-cost API platform for AI image &amp; video generation — GPT-Image-2 from $0.006/image, 160+ images per dollar. One async API covers both image and video: submit a task, get an ID, fetch results via polling or callback. Batch tens of thousands of images without timeouts, switch models without changing code. Pay-as-you-go with no monthly fee — <a href="https://go.apimart.ai/gh-gpt-load">sign up here</a> to get started.</td>
</tr>
</tbody>
</table>

</details>

## Why GPT-Load

Your application only needs one base URL and one AccessKey. Providers, accounts, credentials, models, and routing policy are all configured in the management UI.

<img src="./screenshot/architecture-overview.svg" alt="GPT-Load unified access and upstream routing architecture" width="860">

- **One gateway, native protocols** — Manage official APIs, cloud platforms, model services, and compatible relays together while clients keep their OpenAI, Anthropic, or Gemini native interfaces.
- **One mechanism for API keys and subscriptions** — Codex, Claude, Antigravity, Grok, and API-key channels share credential management, scheduling, and health handling.
- **Scheduling and failure isolation built in** — Multi-credential scheduling, configurable weights, retries, cooldown, blacklisting, and session affinity reduce the impact of overloaded or failing credentials.
- **Observable, self-hosted, and simple to deploy** — Inspect health, routes, logs, usage, and cost estimates in an embedded UI backed by SQLite, MySQL, or PostgreSQL with local credential encryption.

## Quick start

> [!WARNING]
> If you are using 1.x, read [Moving from 1.x](#moving-from-1x) first. 2.0 cannot open, import, or migrate 1.x data in place.

### 1. Start the service

Requires Docker and Docker Compose. This fork defaults to `ghcr.io/0401lucky/gpt-load:latest`, built from `0401lucky/gpt-load` main.

On first use, check the actual GHCR package visibility: public packages allow anonymous pulls; private packages require `docker login ghcr.io` first, using an account with access to the package and a credential with `read:packages` permission.

```bash
git clone --depth 1 --branch main https://github.com/0401lucky/gpt-load.git
cd gpt-load

cp .env.example .env
docker compose up -d
```

Confirm the service is up:

```bash
curl --fail http://127.0.0.1:3001/health
```

The first start generates a management key. Read it and store it safely:

```bash
docker compose exec gpt-load sh -c 'cat /app/data/auth.key'
```

Open <http://127.0.0.1:3001> and sign in to the console with that key.

> You can also set `AUTH_KEY` explicitly in `.env` before starting. By default the service listens on the loopback address only and is not exposed to the internet.

### 2. Initial configuration

Initial setup takes three steps:

1. **Add a channel** — Choose an upstream service and add one or more API keys. For subscription channels, complete the OAuth flow or import credentials as prompted.
2. **Create a group** — Pick a channel, then configure available models and runtime policy.
3. **Create an AccessKey** — Set the groups and client protocols it may use, then give the generated AccessKey to your application.

<details>
<summary>OAuth callback ports for subscription channels</summary>

The Codex, Claude, and Antigravity OAuth clients use fixed callback ports. Compose publishes them on the address configured by `HOST`, which defaults to `127.0.0.1`; setting `HOST=0.0.0.0` also publishes these callback ports on all host interfaces. Because the ports are fixed by the upstream clients, only one default Compose instance can run on a host at a time.

When working over SSH or from a remote browser, the browser's `localhost` may not reach GPT-Load — paste the full callback URL into the authorization dialog to finish the flow.

</details>

## Screenshots

**Groups** — View channels, models, credential counts, traffic, and health in one place

<img src="./screenshot/groups-en-US.png" alt="GPT-Load modern groups overview" width="860">

**Usage statistics** — Review request trends, cache hit rate, token categories, and cost estimates

<img src="./screenshot/usage-en-US.png" alt="GPT-Load modern usage statistics" width="860">

## Scope

### Client protocols

| Protocol                | Main entry                                                         |
| ----------------------- | ------------------------------------------------------------------ |
| OpenAI Chat Completions | `POST /v1/chat/completions`                                        |
| OpenAI Responses        | `/v1/responses` and its resource paths                             |
| OpenAI Images           | `POST /v1/images/...`                                              |
| OpenAI Embeddings       | `POST /v1/embeddings`                                              |
| Rerank                  | `POST /v1/rerank`                                                  |
| Mistral native          | `/v1/ocr`, `/v1/audio/...` |
| Anthropic Messages      | `POST /v1/messages`                                                |
| Gemini                  | `/v1beta/models/...`                                               |
| Gemini Embeddings       | `POST /v1beta/models/{model}:embedContent` / `:batchEmbedContents` |

### Built-in channels

- **Official and cloud** — OpenAI, Anthropic, Gemini, xAI, Azure OpenAI, AWS Bedrock, Google Vertex AI
- **Model services** — DeepSeek, Moonshot AI, SiliconFlow, Zhipu AI, Alibaba, Volcengine, OpenRouter, Cline, Groq, Cerebras, Mistral, Nebius, Parasail, Wafer, Hugging Face (Chat), Cohere (text reranking), OpenCode Go, OpenCode Zen
- **Subscription** — Codex, Claude, Antigravity, Grok
- **Custom** — OpenAI Compatible (any compatible relay)

## Deployment and data

Docker Compose uses application-managed SQLite by default. Data lives in the `gpt-load-data` named volume and includes the database, `auth.key`, and `encryption.key`.

> [!IMPORTANT]
> `encryption.key` decrypts channel credentials. When backing up or migrating, the database and the key **must be kept together**. Once the key is lost or replaced, existing encrypted credentials cannot be recovered, and this version does not support master key rotation.

<details>
<summary>Using an external database</summary>

Use the unified `DATABASE_DSN` to connect SQLite, MySQL, or PostgreSQL:

```text
mysql://user:password@db.example:3306/gpt_load?charset=utf8mb4&collation=utf8mb4_bin
postgres://user:password@db.example:5432/gpt_load?sslmode=require
```

</details>

The fork's `latest` and `main` image tags follow verified main builds for `linux/amd64` and `linux/arm64`. The workflow checks the manifest and runs the image on both native architectures before updating these tags. Versions such as `2.0.0-dev.<run>.g<short-sha>` identify development builds, not upstream releases.

Before updating an existing **2.x** deployment, back up the database and `encryption.key` together. Keep the same deployment directory, Compose project name (`-p` / `COMPOSE_PROJECT_NAME`, if used), `.env`, and data volume containing `auth.key` and `encryption.key`. Pull the configured image and recreate the service in that project:

```bash
docker compose pull gpt-load
docker compose up -d --no-deps gpt-load
```

These commands retain the existing data volume; `docker compose down -v` deletes it. Do not generate replacement keys during an image update. Use `docker compose logs -f` to view logs or `docker compose stop` to stop the service.

To pin a build, set `GPT_LOAD_IMAGE` in the existing `.env` to **one** of the following, replacing the placeholder with a published value from a successful [Main images run](https://github.com/0401lucky/gpt-load/actions/workflows/ghcr-main.yml), then run the same pull/up commands:

```dotenv
GPT_LOAD_IMAGE=ghcr.io/0401lucky/gpt-load:sha-<full-40-character-commit>
# Or pin the immutable multi-platform digest:
GPT_LOAD_IMAGE=ghcr.io/0401lucky/gpt-load@sha256:<64-hex-digest>
```

Commit tags identify the source commit and can be refreshed when that commit is rebuilt; a digest fixes the exact image. Leave `GPT_LOAD_IMAGE` empty to follow `latest` again. An older image does not downgrade the database schema; check data compatibility before rolling back.

**Upstream releases:** `ghcr.io/tbphp/gpt-load:2` and the native binaries linked below follow upstream and may not include this fork's additions. Before GA, upstream `2` tracks verified 2.0 Beta and RC releases; after GA, it tracks stable 2.x releases only. Exact image tags omit the Git tag's `v` prefix (for example, `2.0.0-beta.25`), while `2.0-beta` remains the 2.0 Beta channel. Upstream `ghcr.io/tbphp/gpt-load:latest` remains on 1.x.

<details>
<summary>Using a native binary</summary>

Download the build for your platform from [upstream GitHub Releases](https://github.com/tbphp/gpt-load/releases), and verify it against the bundled `SHA256SUMS` first:

```bash
chmod +x ./gpt-load-linux-amd64

HOST=127.0.0.1 DATA_DIR=./data ./gpt-load-linux-amd64
```

Then open <http://127.0.0.1:3001>. Portable builds are provided for five targets across Linux, macOS (amd64 / arm64), and Windows; `gpt-load-windows-amd64.exe` keeps running in the foreground as before.

Windows desktop users can instead download `gpt-load-windows-setup.exe`. After one administrator approval, Setup installs and starts a low-privilege Windows service, enables automatic startup, and creates desktop and Start Menu shortcuts to the GPT-Load management page. Setup displays the generated management key before it finishes; save it before closing the page. The protected copy remains at `%ProgramData%\GPT-Load\data\auth.key`. Service configuration and its `.env` live in `%ProgramData%\GPT-Load`, with persistent data in `%ProgramData%\GPT-Load\data`.

Installing a newer Setup stops the service gracefully before updating it. Windows uninstall removes the program and service but preserves data. Advanced users can still manage an installed service with `gpt-load-windows-amd64.exe service start|stop|restart|status`.

</details>

### Environment configuration

At startup, the application reads `.env` in the current directory; existing process environment variables take precedence. Unless noted otherwise, changes require restarting the process or container; see [`.env.example`](.env.example) for the common configuration template.

<details>
<summary>Show all environment variables</summary>

| Variable | Default | Description |
| --- | --- | --- |
| `GPT_LOAD_IMAGE` | Empty, uses `ghcr.io/0401lucky/gpt-load:latest` | Compose only; use a full image reference with a commit tag or digest to pin a build. |
| `HOST` | `127.0.0.1` | Native listening address, and the default host address for Compose's main port and OAuth callback ports; Compose always listens on `0.0.0.0` inside the container. |
| `PORT` | `3001` | HTTP service port, must be `1–65535`; Compose also uses it for the container port, host publishing, and health check. |
| `BIND_ADDRESS` | Empty, inherits `HOST` | Compose only; overrides the host publishing address for the main service port without changing OAuth callback ports. |
| `OAUTH_CALLBACK_BIND_ADDRESS` | Empty, inherits `HOST` | Compose only; overrides the host publishing address for the fixed OAuth callback ports `1455`, `54545`, and `51121`. |
| `GRACEFUL_SHUTDOWN_TIMEOUT` | `10` | Maximum time to wait for requests after a stop signal, positive integer in seconds. |
| `CONTAINER_STOP_GRACE_PERIOD` | `15s` | Docker duration to wait before Compose force-stops the container; should be longer than `GRACEFUL_SHUTDOWN_TIMEOUT`. |
| `READ_TIMEOUT` | `60` | HTTP request read timeout, positive integer in seconds. |
| `IDLE_TIMEOUT` | `120` | HTTP keep-alive idle connection timeout, positive integer in seconds. |
| `DATA_DIR` | `./data` | Directory for the managed database, `auth.key`, `encryption.key`, and runtime state; this Compose file uses `/app/data`, while the Windows Setup service uses `%ProgramData%\GPT-Load\data`. |
| `DATABASE_DSN` | Empty, uses `${DATA_DIR}/gpt-load.db` | Empty uses application-managed SQLite; non-empty values support SQLite paths or URLs, MySQL URLs, and PostgreSQL URLs, and are treated as operator-managed external databases. Container file paths must be inside a mounted directory. |
| `DATABASE_MAX_OPEN_CONNECTIONS` | `10` | Maximum open connections for MySQL and PostgreSQL, positive integer. SQLite always uses one connection. |
| `DATABASE_MAX_IDLE_CONNECTIONS` | `5` | Maximum idle connections for MySQL and PostgreSQL, positive integer and no greater than `DATABASE_MAX_OPEN_CONNECTIONS`. SQLite always uses one connection. |
| `AUTH_KEY` | Empty, reads or generates `${DATA_DIR}/auth.key` | Bearer key for the management UI and `/api` management API, not a data-plane AccessKey. |
| `DONATION_INTEGRATION_TOKEN` | Empty, integration disabled | Dedicated 32–256 byte visible-ASCII Bearer secret for `/integrations/donations/v1`. Must differ from the management key and all AccessKeys; restart after changing it. |
| `ENCRYPTION_KEY` | Empty, reads or generates `${DATA_DIR}/encryption.key` | Encrypts channel credentials; changing or losing it makes existing credentials undecryptable, so back it up with the database. |
| `CLIENT_IP_HEADER` | Empty, uses the connection IP | Client IP header, such as `X-Forwarded-For` or `CF-Connecting-IP`; missing or invalid values fall back to the connection IP. Shared by logs, AccessKey IP restrictions, and other IP consumers; supports IPv4/IPv6. Restart after changes. |
| `TRUSTED_PROXIES` | Empty | Optional comma-separated proxy IPs or CIDRs; applies only with `CLIENT_IP_HEADER`. Empty means trusting the selected header directly, with trust enforced by your deployment. When set, only matching connection peers may supply the header; others use their connection IP. For `X-Forwarded-For`, scan right to left for the first untrusted IP (leftmost if all are trusted); without a list, use the leftmost IP. Other headers accept one IP only. Restart after changes. |
| `HTTP_PROXY` | Empty | Environment proxy for HTTP upstream requests. |
| `HTTPS_PROXY` | Empty | Environment proxy for HTTPS upstream requests. |
| `NO_PROXY` | Empty | Comma-separated hosts, domains, or IPs that bypass the environment proxy. |
| `LOG_LEVEL` | `info` | Supports `panic`, `fatal`, `error`, `warn`, `warning`, `info`, `debug`, and `trace`; invalid values warn and fall back to `info`. |
| `LOG_FORMAT` | `text` | Supports `text` and `json`; any other value fails startup. |
| `MODELS_DEV_AUTO_SYNC_ENABLED` | Unset, initial default `true` | When unset, uses the persisted management UI setting; when set, forces Models.dev auto-sync on or off and makes the same UI option read-only. |

Environment proxies apply only when no proxy is specified on the credential, group, or global settings.

</details>

## Production considerations

Donation intake supports ordinary API keys, including Gemini AI Studio, using an existing group's channel and validation configuration. Its restricted integration API provides capabilities, group options, encrypted batch staging, per-item receipts, and retries by the original batch/item identifiers. A successful request only confirms durable staging; only an `accepted` item has passed its own key probe and completed credential/runtime publication. new-api owns community identity, activities, and permanent rewards.

Each request accepts up to 100 items, 4096 bytes per key, and a 1 MiB JSON body. Automatic probes have five attempts; a dedicated retry action can resume retained staging without resending keys. Unaccepted staging ciphertext expires after seven days. Acquisition fingerprints and original receipts remain after credential/group deletion and token rotation; back up the database and encryption key together. Clearing `DONATION_INTEGRATION_TOKEN` disables intake and its worker while retaining history.

A batch may instead be submitted with `validation_mode: "manual_review"`. Capabilities then advertise `features: ["manual_review_v1"]` and a separate `review_limits` block, and each batch item is only encrypted and held in `pending_review`: it never enters the automatic probe path, never takes inventory ownership early, and never grants credit on its own. A `manual_review` request must freeze the group's `manual_target_revision`, which is a separate HMAC over the effective execution configuration; renaming a group does not invalidate it. Older callers that omit the field keep the original `auto` behaviour and the original request comparator byte for byte.

While an item waits, an administrator can read `GET /batches/:batch_id/items/:item_id/review-context`, run a real chat completion against that item's own staged key with `POST /batches/:batch_id/items/:item_id/tests`, and then decide with `POST /batches/:batch_id/items/:item_id/review-actions` (`enter_review`, `approve` or `reject`). Every action carries a caller-supplied `action_id` that doubles as its `Idempotency-Key`, so a lost response is reconciled against the stored command instead of being applied twice. A test only ever uses that item's key: it never falls back to another credential or group, it does not pass through the normal relay, and it does not touch any wallet. Only an approval that also commits the credential and finishes runtime publication produces `accepted`; the test outcome itself never approves or rejects anything.

`approve` and `reject` are mutually exclusive with a running test, an item past its staging deadline cannot be approved, and a rejection never requires the target group to be online. An item whose automatic probes were exhausted while staging is still retained can be converted explicitly with `enter_review`; the original batch, request summary, attempt budget, donor and deadline are preserved. Staging never extends: conversion, tests and retries all keep the original seven-day deadline.

See `internal/control/donation_*.go` and `internal/storage/migrations/0019_donation_manual_review.go` for the enforced contract.

- The service listens on `127.0.0.1` only by default. For remote access, expose it through a controlled network or a TLS reverse proxy, and configure ACLs and firewall rules.
- Manage `AUTH_KEY` and `ENCRYPTION_KEY` carefully. Never commit real keys to a repository, log, screenshot, or public issue.
- 2.0 is designed for a **single application instance**. Instances do not share state, so horizontal scaling is not supported.
- Usage and cost are **estimates** derived from upstream responses. They support operational analysis and capacity planning, and do not equal a provider invoice or a financial reconciliation.
- Group model usage advances its cycle only on explicit quota reset evidence, not ordinary retry cooldowns. After an upgrade, legacy cycles with unverifiable sources fall back to the last 24 hours. Missing or partial usage is flagged separately; unknown tokens are not filled in automatically.
- Subscription channels depend on upstream OAuth and compatibility protocols and may change as upstreams change. Only connect accounts you are entitled to use, and follow each provider's terms.
- `responses_websocket_enabled` defaults to enabled. An explicit group setting overrides the global value; otherwise the group inherits it. Disabling immediately closes affected WS connections and interrupts generation without affecting HTTP/SSE. Re-enabling does not restore the old connection's temporary state.
- `rate_limit_reset_hint_enabled` is a group-level switch and defaults to disabled. When enabled, an upstream 429 that states when the model resets (for example `Try again in 3h 18m`) cools that model on that credential until the stated time plus a one-minute margin instead of the default one minute; the result is clamped to between one minute and 24 hours, and a missing or unparseable hint keeps the default. Only that model on that credential is affected. While every usable credential of a model is cooling, the group answers 429 with `Retry-After` and the unchanged `upstream_rate_limited` code instead of dispatching upstream. Clear the switch before rolling back to an image that does not know the key, because such an image rejects the group configuration.
- `empty_response_retry` defaults to disabled and can be overridden per group. When enabled, an empty streaming chat response may retry another candidate within the shared retry budget, without cooling or blacklisting the credential. Prewarming, continuation, and WebSocket requests are excluded.
- `concurrency_limit` is the group data-plane concurrency cap and inherits `default_group_concurrency_limit` when unset; `0` means unlimited. Global and AccessKey caps still apply, and requests are rejected immediately when a cap is reached.
- `codex_live_mode` supports `off`, `direct` (default), and `relay`, with group overrides. Direct mode requires the client to reach the upstream; relay mode requires a public media address and UDP ports. Disabling voice does not disable text requests.

## Moving from 1.x

> [!WARNING]
> GPT-Load 2.0 is a complete rewrite. It **cannot** open, import, or migrate 1.x data in place.

Deploy 2.0 with its own database, `DATA_DIR`, port, and Docker volume. Cut traffic over only after verification, and keep the original 1.x deployment until the rollback window closes. Documentation for the 1.4.x maintenance line is at the [official docs](https://www.gpt-load.com/docs).

## Open-source dependencies

Some of GPT-Load's capabilities build on these projects, with thanks:

| Project                                                     | Role                                                                                 | License    |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------ | ---------- |
| [Bifrost Core](https://github.com/maximhq/bifrost)          | Provider authentication, request/response conversion, streaming, usage normalization | Apache-2.0 |
| [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) | OAuth and execution adapter for subscription channels                                | MIT        |
| [Lobe Icons](https://github.com/lobehub/lobe-icons)         | Channel brand icons in the management UI                                             | MIT        |

GPT-Load owns credential storage, account selection, scheduling, retry, health, affinity, logging, and usage policy. Third-party notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md), full license texts in [`LICENSES/`](LICENSES/), and each release ships a CycloneDX SBOM covering the Go dependency graph.

Channel icons identify their respective upstream providers. All trademarks belong to their owners; this project is not affiliated with or endorsed by them.

## Project support

<table>
<tbody>
<tr>
<td align="center" width="33%">
<a href="https://openai.com/">
<picture>
<source media="(prefers-color-scheme: dark)" srcset="./screenshot/sponsor-openai-lockup-white.svg">
<source media="(prefers-color-scheme: light)" srcset="./screenshot/sponsor-openai-lockup-black.svg">
<img src="./screenshot/sponsor-openai-lockup-black.svg" alt="OpenAI" width="120">
</picture>
</a>
<br><sub>Platform support</sub>
</td>
<td align="center" width="33%">
<a href="https://linux.do"><img src="./screenshot/l.png" alt="LINUX DO" width="120"></a>
<br><sub>Community support</sub>
</td>
<td align="center" width="33%">
<a href="https://www.digitalocean.com/?refcode=3d52cff21342&utm_campaign=Referral_Invite&utm_medium=Referral_Program&utm_source=badge"><img src="https://web-platforms.sfo2.cdn.digitaloceanspaces.com/WWW/Badge%202.svg" alt="DigitalOcean" width="120"></a>
<br><sub>Infrastructure support</sub>
</td>
</tr>
</tbody>
</table>

---

[MIT License](LICENSE) · [Third-party notices](THIRD_PARTY_NOTICES.md) · [Security policy](SECURITY.md)
