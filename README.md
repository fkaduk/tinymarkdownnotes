# Tiny Markdown Notes

A small Go application for creating and sharing public Markdown notes

## Run locally

```bash
NOTES_ADMIN_KEY=change-me make run
```

Open <http://localhost:5000>

Notes are stored in `data/notes.db`

## Test and build

```bash
npm ci
npx playwright install chromium
make all
```

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:5000` | HTTP listen address |
| `DATA_DIR` | `data` | Data directory |
| `NOTES_DB_PATH` | `data/notes.db` | SQLite database path |
| `NOTES_ADMIN_KEY` | insecure development value | Password for creating notes |

## Container image

Tagged releases are published as:

```text
ghcr.io/fkaduk/tinymarkdownnotes:<version>
```

The container listens on port `5000` and stores its database under `/app/data`

## Release

```bash
make tag
```
