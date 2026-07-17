# Tiny Markdown Notes

A dead-simple way to share public markdown notes.

## Deployment

Set up your environment variables:

```bash
cp .env.example .env
# Edit .env with your values
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
go test ./...
go run .
```

The app stores notes in `data/notes.db`. On startup it imports existing
`notes/*.json` files without overwriting rows that already exist in SQLite.
