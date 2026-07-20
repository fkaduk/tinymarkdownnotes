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
make up
```

The app is available at `http://localhost:5000`.

For production HTTPS and rate limiting, set these values in `.env`:

```dotenv
COMPOSE_PROFILES=production
DOMAIN=notes.example.com
```

Then run `make up`. The same Compose file starts Caddy when the `production`
profile is enabled. Port 5000 remains bound to the host loopback interface;
public traffic enters through Caddy on ports 80 and 443.

Useful commands:

```bash
make ps
make logs
make restart
make down
```

## Development

This project uses Go and SQLite:

```bash
make check
make run
```

The app stores notes in `data/notes.db`.

Run `make help` to see the build, formatting, linting, coverage, benchmark,
end-to-end test, and Compose targets. Variables are overrideable for larger
repositories; for example, `make build APP_NAME=api CMD_PATH=./cmd/api`.

### End-to-end tests

Install the JavaScript dependencies and the Chromium test browser once:

```bash
make setup-e2e
```

Run the end-to-end suite:

```bash
make test-e2e
```

Playwright starts the Go application automatically on port 4173 with an
isolated SQLite database under `test-results/`.
