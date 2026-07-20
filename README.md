# Tiny Markdown Notes

A dead-simple way to share public markdown notes.

## Deployment

Set up your environment variables:

```bash
cp .env.example .env
# Edit .env and set a real NOTES_ADMIN_KEY before starting Compose
```

For local deployment, run:

```bash
docker compose up -d --build
```

Use `docker-compose` instead if Compose is installed as a standalone command.

The app is available at `http://localhost:5000`.

For production HTTPS and rate limiting, set these values in `.env`:

```dotenv
COMPOSE_PROFILES=production
DOMAIN=notes.example.com
```

Then run `docker compose up -d --build`. The same Compose file starts Caddy
when the `production` profile is enabled. Port 5000 remains bound to the host
loopback interface; public traffic enters through Caddy on ports 80 and 443.

Useful commands:

```bash
docker compose ps
docker compose logs -f
docker compose restart
docker compose down
```

## Development

This project uses Go and SQLite:

```bash
make audit
make test
make build
make run
```

The app stores notes in `data/notes.db`.

`make test` runs both Go and Playwright tests. `make build` writes the native
binary to `bin/tinymarkdownnotes`.

Plain `make` and `make all` run the same audit, test, and build sequence used by
CI:

```bash
make all
```

The project prefers Go 1.26.5 while retaining Go 1.25 language compatibility.
Node.js 24.18.0 is pinned in `.node-version`.

### End-to-end tests

Install the JavaScript dependencies and the Chromium test browser once:

```bash
npm ci
npx playwright install chromium
```

Run the end-to-end suite:

```bash
make test
```

Playwright starts the Go application automatically on port 4173 with an
isolated SQLite database under `test-results/`.
