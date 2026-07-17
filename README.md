# Tiny Markdown Notes

A dead-simple way to share public markdown notes.

## Deployment

Set up your environment variables:

```bash
cp .env.example .env
# Edit .env with your values
```

For **base deployment**, run:

```bash
make init
docker-compose up -d
```

and access the app at `http://localhost:5000`

For **production deployment** with HTTPS and rate limiting, run:

```bash
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

a access the app at your selected domain.

## Development

This project uses Go and SQLite:

```bash
go test ./...
go run .
```

The app stores notes in `data/notes.db`. On startup it imports existing
`notes/*.json` files without overwriting rows that already exist in SQLite.
