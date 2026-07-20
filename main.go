package main

import (
	"context"
	"database/sql"
	_ "embed"
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
	CreatedAt string
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
	db.SetMaxOpenConns(1) // concurrent writes are not a good idea for sqlite

	app := &App{db: db, adminKey: adminKey}
	if err := app.configureDatabase(); err != nil {
		db.Close()
		return nil, err
	}
	if err := app.loadTemplates(); err != nil {
		db.Close()
		return nil, err
	}
	return app, nil
}

// Close releases resources held by the application.
func (a *App) Close() error {
	return a.db.Close()
}

// configureDatabase applies SQLite settings and creates the schema.
func (a *App) configureDatabase() error {
	if _, err := a.db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("enable wal: %w", err)
	}
	if _, err := a.db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	_, err := a.db.Exec(`
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

// loadTemplates parses HTML templates once during startup.
func (a *App) loadTemplates() error {
	// FuncMap exposes small Go helpers to templates
	funcs := template.FuncMap{
		// staticURL centralizes the public URL prefix for CSS and other assets.
		// TODO: is this really necessary? isnt there something build-in for the template pkg ?
		"staticURL": func(name string) string {
			return "/static/" + strings.TrimLeft(name, "/")
		},
	}
	tmpl, err := template.New("").Funcs(funcs).ParseGlob("templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	a.templates = tmpl
	return nil
}

// Routes builds the HTTP handler tree.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	handler := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("GET /static/", handler)

	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("POST /notes", a.requireAuth(a.handleCreateNote))
	mux.HandleFunc("GET /notes/{slug}", a.handleViewNote)
	mux.HandleFunc("POST /notes/{slug}", a.handleUpdateNote)

	return mux
}

// requireAuth accepts a handler and returns a new handler that
// performs authentication before calling the original one.
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if !ok || password != a.adminKey {
			w.Header().Set("WWW-Authenticate", `Basic realm="Tiny Markdown Notes"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// handleIndex renders the note-creation form.
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "index.html", nil)
}

// handleCreateNote validates a submitted HTML form and inserts a new note.
func (a *App) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if !validateSlug(slug) {
		alertBack(w, http.StatusBadRequest, "Invalid note name")
		return
	}

	markdown := fmt.Sprintf("# %s\n%s", slug, initNoteContent)
	res, err := a.db.ExecContext(
		r.Context(),
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
	http.Redirect(w, r, "/notes/"+slug, http.StatusSeeOther)
}

// handleViewNote loads one note and renders the main note page.
func (a *App) handleViewNote(w http.ResponseWriter, r *http.Request) {
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

	a.render(w, http.StatusOK, "note.html", map[string]any{
		"Slug": slug,
		"Note": note,
	})
}

// handleUpdateNote saves an edit only if the browser edited the current version.
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
	if len(markdown) > maxMarkdownBytes {
		http.Error(w, "Note content too large", http.StatusRequestEntityTooLarge)
		return
	}
	clientVersion, err := strconv.Atoi(r.FormValue("version"))
	if err != nil || clientVersion < 1 {
		http.Error(w, "Invalid note version", http.StatusBadRequest)
		return
	}

	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Update note failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

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

	// TODO: dont like this, happy path should be left aligned.
	if rows == 1 {
		// Exactly one affected row means both slug and version matched.
		if err := tx.Commit(); err != nil {
			http.Error(w, "Update note failed", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/notes/"+slug, http.StatusSeeOther)
		return
	}

	// A zero-row update means the submitted version is stale. Keep the stored note
	// unchanged and let the browser preserve the rejected draft for the user.
	http.Error(w, "Someone else saved this note first. Your changes were not saved.", http.StatusConflict)
}

// getNote isolates the repeated SELECT-and-Scan logic. Its three return values
// distinguish "found", "not found", and "database failure" without using an
// error for the expected not-found case.
func (a *App) getNote(ctx context.Context, slug string) (Note, bool, error) {
	var note Note

	err := a.db.QueryRowContext(
		ctx,
		`SELECT slug, markdown, version, created_at, updated_at FROM notes WHERE slug = ?`,
		slug,
	).Scan(&note.Slug, &note.Markdown, &note.Version, &note.CreatedAt, &note.UpdatedAt)
	// TODO: i dont get what this bool is for. we know from the Note and error value how things went.
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, false, nil
	}
	if err != nil {
		return Note{}, false, err
	}
	return note, true, nil
}

// render writes one parsed HTML template to the HTTP response.
// TODO: func name insufficiently rescriptive
func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	// TODO: do we use data type any here because we just propagate the ExecueTemplate arg?
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render template", "template", name, "error", err)
	}
}

// getenv returns the value of the environment variable named by key after
// removing leading and trailing whitespace. If the variable is unset or the
// trimmed value is empty, getenv returns fallback.
func getenv(key, fallback string, log bool) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		value = fallback
	}
	if log {
		slog.Info("Configuration set - ", "key", key, "value", value)
	}
	return value
}

// validateSlug enforces note-name rules.
func validateSlug(slug string) bool {
	return slugPattern.MatchString(slug)
}

// alertBack produces a tiny HTML response for form errors on the creation page.
// %q quotes and escapes the message before embedding it as a JavaScript string.
// TODO: dont like the name of this function, not descriptive enought
func alertBack(w http.ResponseWriter, status int, message string) {
	// TODO: is this really the best approach ? wouldnt a browser level alert suffice ?
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `
			<script>
				alert(%q);
				window.history.back();
			</script>
		`, message)
}
