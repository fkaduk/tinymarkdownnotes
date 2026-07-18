package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultAdminKey  = "change-me-in-production"
	maxMarkdownBytes = 100_000
)

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

//go:embed templates/init_note.md
var initNoteContent string

type App struct {
	db        *sql.DB
	templates *template.Template
	adminKey  string
}

type Note struct {
	Slug      string
	Markdown  string
	Version   int
	CreatedAt string // TODO: use actual timestamps in the db ?
	UpdatedAt string
}

func main() {
	addr := getenv("ADDR", ":5000", true)
	dataDir := getenv("DATA_DIR", "data", true)
	dbPath := getenv("NOTES_DB_PATH", filepath.Join(dataDir, "notes.db"), true)
	adminKey := getenv("NOTES_ADMIN_KEY", defaultAdminKey, false)
	if adminKey == defaultAdminKey {
		slog.Warn("NOTES_ADMIN_KEY is using the insecure default")
	}

	app, err := NewApp(dbPath, adminKey)
	if err != nil {
		slog.Error("initialize app", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("serve", "error", err)
		if err := app.Close(); err != nil {
			slog.Error("close app", "error", err)
		}
		os.Exit(1)
	}
}

// NewApp constructs an application backed by dbPath and using adminKey for
// authentication.
func NewApp(dbPath, adminKey string) (*App, error) {
	if dbPath == "" {
		return nil, errors.New("database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1) // avoid sqlite write issues

	app := &App{db: db, adminKey: adminKey}
	// Startup work uses a background context because it is not associated with an
	// incoming request. Request handlers use r.Context() instead, so their database
	// operations are cancelled if the client disconnects.
	if err := app.configureDatabase(context.Background()); err != nil {
		// A partially initialized App is not returned, so NewApp must release the
		// resources it has already acquired on every error path.
		db.Close()
		return nil, err
	}
	if err := app.loadTemplates(); err != nil {
		db.Close()
		return nil, err
	}
	return app, nil
}

// getenv returns the value of the environment variable named by key after
// removing leading and trailing whitespace. If the variable is unset or the
// trimmed value is empty, getenv returns fallback.
func getenv(key, fallback string, logValue bool) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		value = fallback
	}
	if logValue {
		slog.Info("Configuration set - ", "key", key, "value", value)
	}
	return value
}

// Close releases resources held by the application.
func (a *App) Close() error {
	return a.db.Close()
}

// configureDatabase applies SQLite settings and creates the schema.
func (a *App) configureDatabase(ctx context.Context) error {
	if _, err := a.db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("enable wal: %w", err)
	}
	if _, err := a.db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	_, err := a.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS notes (
			slug TEXT PRIMARY KEY,
			markdown TEXT NOT NULL,
			version INTEGER NOT NULL CHECK (version > 0),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// loadTemplates parses every HTML template once during startup.
func (a *App) loadTemplates() error {
	// FuncMap exposes small Go helpers to templates. It must be attached before
	// ParseGlob because templates resolve function names while they are parsed.
	funcs := template.FuncMap{
		// staticURL centralizes the public URL prefix for CSS and other assets.
		"staticURL": func(name string) string {
			return "/static/" + strings.TrimLeft(name, "/")
		},
		// toJSON safely serializes server data for use as a JavaScript value. JSON
		// encoding is essential here; interpolating raw Markdown could break the
		// script or turn note content into executable JavaScript.
		"toJSON": func(v any) (template.JS, error) {
			b, err := json.Marshal(v)
			return template.JS(b), err
		},
	}
	tmpl, err := template.New("").Funcs(funcs).ParseGlob("templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	a.templates = tmpl
	return nil
}

// Routes builds the HTTP handler tree. Go 1.22+ ServeMux patterns can include an
// HTTP method and named path wildcards such as {slug}.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	// FileServer expects paths relative to its directory. StripPrefix converts a
	// request such as /static/style.css into style.css before the file lookup.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	// Reading a note is public; creating one is the only route protected by Basic
	// Auth. Handler methods are passed as function values.
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("POST /notes", a.requireAuth(a.handleCreateNote))
	mux.HandleFunc("GET /notes/{slug}", a.handleViewNote)
	mux.HandleFunc("POST /notes/{slug}", a.handleUpdateNote)
	mux.HandleFunc("GET /notes/{slug}/meta", a.handleNoteMeta)

	// Wrap the mux with a small application-wide path check. The returned
	// http.HandlerFunc itself implements http.Handler through its ServeHTTP method.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.EscapedPath(), "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// requireAuth is middleware: it accepts a handler and returns a new handler that
// performs authentication before optionally calling the original one.
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The username is intentionally ignored; this app treats the configured
		// admin key as the only credential.
		_, password, ok := r.BasicAuth()
		if !ok || password != a.adminKey {
			w.Header().Set("WWW-Authenticate", `Basic realm="Tiny Markdown Notes"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Calling next continues the request pipeline only after authentication.
		next(w, r)
	}
}

// validateSlug is shared by create, view, and update paths so all entry
// points enforce exactly the same note-name rules.
func validateSlug(slug string) bool {
	return slugPattern.MatchString(slug)
}

// handleIndex renders the note-creation form.
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	// The GET / pattern is a subtree match in ServeMux, so explicitly reject paths
	// that did not match a more specific route.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	a.render(w, http.StatusOK, "index.html", nil)
}

// handleCreateNote validates a submitted HTML form and inserts a new note.
func (a *App) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	// ParseForm populates r.Form from application/x-www-form-urlencoded or
	// multipart form data. FormValue below reads from that parsed collection.
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if !validateSlug(slug) {
		alertBack(w, http.StatusBadRequest, "Invalid note name")
		return
	}

	// SQL placeholders (?) keep user input separate from the SQL program. Never
	// build SQL by concatenating form values.
	markdown := fmt.Sprintf("# %s\n%s", slug, initNoteContent)
	res, err := a.db.ExecContext(
		r.Context(),
		// OR IGNORE turns a duplicate primary key into zero affected rows, which
		// lets the handler report a friendly "already exists" response.
		`INSERT OR IGNORE INTO notes (slug, markdown, version) VALUES (?, ?, 1)`,
		slug,
		markdown,
	)
	if err != nil {
		http.Error(w, "Create note failed", http.StatusInternalServerError)
		return
	}
	rows, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Create note failed", http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		alertBack(w, http.StatusConflict, "Note already exists")
		return
	}
	// Post/Redirect/Get prevents a browser refresh from submitting the creation
	// form again. 303 tells the browser to follow the redirect with GET.
	http.Redirect(w, r, "/notes/"+slug, http.StatusSeeOther)
}

// handleViewNote loads one note and renders the main note page.
func (a *App) handleViewNote(w http.ResponseWriter, r *http.Request) {
	// PathValue returns the portion captured by {slug} in the route pattern.
	slug := r.PathValue("slug")
	if !validateSlug(slug) {
		http.Error(w, "Invalid note slug", http.StatusBadRequest)
		return
	}
	note, ok, err := a.getNote(r.Context(), slug)
	if err != nil {
		http.Error(w, "Load note failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	// A map is convenient for small, template-specific view data. Dot expressions
	// in note.html access these values as .Slug and .Note.
	a.render(w, http.StatusOK, "note.html", map[string]any{
		"Slug": slug,
		"Note": note,
	})
}

// handleNoteMeta returns lightweight JSON used by the browser's version poll.
// It avoids downloading and rendering the full note merely to detect a change.
func (a *App) handleNoteMeta(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validateSlug(slug) {
		http.Error(w, "Invalid note slug", http.StatusBadRequest)
		return
	}
	note, ok, err := a.getNote(r.Context(), slug)
	if err != nil {
		http.Error(w, "Load note failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	// Set response headers before writing the body. Encode streams JSON directly
	// to the ResponseWriter and appends a newline.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":    note.Version,
		"updated_at": note.UpdatedAt,
	})
}

// handleUpdateNote saves an edit only if the browser edited the current version.
// This technique is called optimistic concurrency control.
func (a *App) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validateSlug(slug) {
		http.Error(w, "Invalid note slug", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	markdown := r.FormValue("markdown")
	// len(string) is measured in bytes, which is why the limit's unit is explicit
	// in the constant name.
	if len(markdown) > maxMarkdownBytes {
		http.Error(w, "Note content too large", http.StatusRequestEntityTooLarge)
		return
	}
	clientVersion, err := strconv.Atoi(r.FormValue("version"))
	if err != nil {
		// Version zero cannot match a valid row (the schema requires version > 0),
		// so malformed or missing input safely follows the conflict path.
		clientVersion = 0
	}

	// A transaction groups the conditional update and its commit into one unit.
	// The deferred rollback is a safety net: after Commit it becomes a harmless
	// no-op, while every early return automatically abandons the transaction.
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Update note failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// The version comparison happens inside the UPDATE, atomically in SQLite.
	// Two clients can submit version 1 simultaneously, but only the first update
	// changes the row to version 2; the second then matches zero rows.
	res, err := tx.ExecContext(
		r.Context(),
		`UPDATE notes
		 SET markdown = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE slug = ? AND version = ?`,
		markdown,
		slug,
		clientVersion,
	)
	if err != nil {
		http.Error(w, "Update note failed", http.StatusInternalServerError)
		return
	}
	rows, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Update note failed", http.StatusInternalServerError)
		return
	}
	if rows == 1 {
		// Exactly one affected row means both slug and version matched.
		if err := tx.Commit(); err != nil {
			http.Error(w, "Update note failed", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/notes/"+slug, http.StatusSeeOther)
		return
	}

	// The browser normally detects staleness through polling. This 409 remains the
	// authoritative guard for the small race window between polls.
	http.Error(w, "Someone else saved this note first. Your changes were not saved.", http.StatusConflict)
}

// getNote isolates the repeated SELECT-and-Scan logic. Its three return values
// distinguish "found", "not found", and "database failure" without using an
// error for the expected not-found case.
func (a *App) getNote(ctx context.Context, slug string) (Note, bool, error) {
	var note Note
	// Scan requires destination pointers so it can assign the selected columns.
	// Their order must match the SELECT list below.
	err := a.db.QueryRowContext(
		ctx,
		`SELECT slug, markdown, version, created_at, updated_at FROM notes WHERE slug = ?`,
		slug,
	).Scan(&note.Slug, &note.Markdown, &note.Version, &note.CreatedAt, &note.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, false, nil
	}
	if err != nil {
		return Note{}, false, err
	}
	return note, true, nil
}

// render writes one parsed HTML template to the HTTP response.
func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render template", "template", name, "error", err)
	}
}

// alertBack produces a tiny HTML response for form errors on the creation page.
// %q quotes and escapes the message before embedding it as a JavaScript string.
func alertBack(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `
			<script>
				alert(%q);
				window.history.back();
			</script>
		`, message)
}
