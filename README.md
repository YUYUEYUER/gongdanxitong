<a href="https://zerodha.tech"><img src="https://zerodha.tech/static/images/github-badge.svg" align="right" alt="Zerodha Tech Badge" /></a>


# libredesk

Modern, open source, self-hosted omnichannel customer support desk. Live chat, email, and more in a single binary.

![image](https://libredesk.io/hero-dark-v2.png?q=2)


Visit [libredesk.io](https://libredesk.io) for more info. Check out the [**live demo**](https://demo.libredesk.io/).

## Features

- **Omnichannel inbox**  
  Live chat, email, and more — all in one inbox. Connect support@, billing@, sales@ and manage every conversation from a single, unified interface.
- **Live chat widget**  
  Embed a real-time chat widget on your website. Engage visitors instantly and handle live conversations right from your support desk.
- **Granular permissions**  
  Create custom roles with granular permissions for teams and individual agents.
- **Automations**  
  Eliminate repetitive tasks with powerful automation rules. Auto-tag, assign, and route conversations based on custom conditions.
- **CSAT surveys**  
  Measure customer satisfaction with automated surveys.
- **Macros**  
  Save frequently sent messages as templates. With one click, send saved responses, set tags, and more.
- **Organization**  
  Keep conversations organized with tags, custom statuses for conversations, and snoozing. Find any conversation instantly from the search bar.
- **Auto assignment**  
  Distribute workload with auto assignment rules. Auto-assign conversations based on agent capacity or custom criteria.
- **SLA management**  
  Set and track response time targets. Get notified when conversations are at risk of breaching SLA commitments.
- **Custom attributes**  
  Create custom attributes for contacts or conversations such as the subscription plan or the date of their first purchase. 
- **AI-assist**  
  Instantly rewrite responses with AI to make them more friendly, professional, or polished.
- **Activity logs**  
  Track all actions performed by agents and admins—updates and key events across the system—for auditing and accountability.
- **Webhooks**  
  Integrate with external systems using real-time HTTP notifications for conversation and message events.
- **Command bar**  
  Opens with a simple shortcut (CTRL+K) and lets you quickly perform actions on conversations.

And more — checkout [libredesk.io](https://libredesk.io) or try the [live demo](https://demo.libredesk.io/).


## Installation

### Docker

Production deployments must use an immutable release image reference containing both a version tag and its `sha256` manifest digest. Mutable tags such as `latest` or `main` are not supported for production upgrades.

```shell
# Download the deployment files in the current directory.
curl -LO https://github.com/YUYUEYUER/gongdanxitong/raw/main/docker-compose.yml
curl -LO https://github.com/YUYUEYUER/gongdanxitong/raw/main/config.sample.toml
curl -LO https://github.com/YUYUEYUER/gongdanxitong/raw/main/.env.example

# Create the local configuration files.
cp config.sample.toml config.toml
cp .env.example .env

# Set LIBREDESK_IMAGE to an immutable release reference containing both a tag
# and @sha256 digest. Set independent random values for LIBREDESK_ENCRYPTION_KEY
# (32 characters), POSTGRES_PASSWORD, and REDIS_PASSWORD in .env. Also set
# app.root_url in config.toml and configure trusted proxy CIDRs when a reverse
# proxy is used.

# Run the services in the background.
docker compose pull
docker compose up -d

# Setting System user password.
docker exec -it libredesk_app ./libredesk --set-system-user-password
```

Go to `http://localhost:9000` and login with username `System` and the password you set using the `--set-system-user-password` command. Production deployments must use HTTPS. Livechat inboxes must list every embedding host in `trusted_domains`; an empty list permits same-origin embedding only.

LibreDesk is currently a single-organization, single-application-replica system. Do not scale the `app` service above one process, and do not treat inboxes or teams as tenant boundaries. Use separate deployments, databases, Redis instances, encryption keys, and upload stores for mutually distrusting organizations.

### Security upgrade to v2.5.2

Do not run old and new binaries against the same database during this upgrade.

1. Stop every old LibreDesk application instance. Keep PostgreSQL, Redis, and the upload store available.
2. Back up PostgreSQL and the complete upload store. Verify that the backup can be restored.
3. Pin `LIBREDESK_IMAGE` to the new release tag and digest, then pull it.
4. Run exactly one migration process: `docker compose run --rm --no-deps app /libredesk/libredesk --upgrade --yes --config /libredesk/config.toml`.
5. Start only the new application image with `docker compose up -d app` and verify `/readyz` before restoring traffic.

The migration revokes all agent sessions and API keys. It also clears passwords for contacts affected by the legacy portal registration flow; those customers must use the verified password-reset email flow. Existing Widget Redis sessions are rejected by the new versioned session format. Do not roll the application binary back unless the database and upload store are restored to the matching pre-upgrade backup.

See [installation docs](https://docs.libredesk.io/getting-started/installation)

__________________

### Binary
- Download the [latest release](https://github.com/abhinavxd/libredesk/releases) and extract the libredesk binary.
- Edit config.toml as needed.
- `./libredesk --install` to setup the Postgres DB.
- Run `./libredesk --set-system-user-password` to set the password for the System user.
- Run `./libredesk` and visit `http://localhost:9000` and login with email `System` and the password you set using the --set-system-user-password command.

See [installation docs](https://docs.libredesk.io/getting-started/installation)
__________________

## Developers

- If you are interested in contributing, **please read [CONTRIBUTING.md](./CONTRIBUTING.md) first**.
- For local development and setup, refer to the [developer setup](https://docs.libredesk.io/contributing/developer-setup).
- For planned features and project direction, see [ROADMAP.md](./ROADMAP.md).

The backend is written in Go and the frontend is Vue.js 3 with Shadcn UI.

### Turnstile and Registration Security

Customer registration can be protected with Cloudflare Turnstile, CSRF validation, and registration-specific rate limits.

```env
TURNSTILE_SITE_KEY=your_site_key
TURNSTILE_SECRET_KEY=your_secret_key
TURNSTILE_VERIFY_URL=https://challenges.cloudflare.com/turnstile/v0/siteverify
TURNSTILE_VERIFY_TIMEOUT_MS=5000
REGISTER_RATE_LIMIT_WINDOW_SECONDS=300
REGISTER_RATE_LIMIT_MAX_ATTEMPTS=5
```

`TURNSTILE_SITE_KEY` is exposed through the public app config so the Vue registration page can render the widget. `TURNSTILE_SECRET_KEY` is only read by the Go backend and must never be committed or sent to the frontend. `.env` is ignored by Git; use `.env.example` as the local template.



## Translators
You can help translate libredesk into your language on [Crowdin](https://crowdin.com/project/libredesk).  
