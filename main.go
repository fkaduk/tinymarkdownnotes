package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

const maxMarkdownSize = 100_000

type Config struct {
	Addr      string
	DBPath    string
	ImportDir string
	AdminKey  string
}

type App struct {
	db          *sql.DB
	templates   *template.Template
	adminKey    string
	initContent string
}

type Note struct {
	Slug      string
	Markdown  string
	Version   int
	CreatedAt string
	UpdatedAt string
}

type noteJSON struct {
	Markdown string `json:"markdown"`
	Version  int    `json:"version"`
}

func main() {
	cfg := configFromEnv()
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", cfg.Addr)
	log.Fatal(server.ListenAndServe())
}

func configFromEnv() Config {
	dataDir := getenv("DATA_DIR", "data")
	return Config{
		Addr:      getenv("ADDR", ":5000"),
		DBPath:    getenv("NOTES_DB_PATH", filepath.Join(dataDir, "notes.db")),
		ImportDir: getenv("NOTES_IMPORT_DIR", "notes"),
		AdminKey:  getenv("NOTES_ADMIN_KEY", "change-me-in-production"),
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func NewApp(cfg Config) (*App, error) {
	if cfg.DBPath == "" {
		return nil, errors.New("database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", cfg.DBPath+"?_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	app := &App{db: db, adminKey: cfg.AdminKey}
	if err := app.configureDatabase(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := app.loadTemplates(); err != nil {
		db.Close()
		return nil, err
	}
	if err := app.loadInitContent(); err != nil {
		db.Close()
		return nil, err
	}
	if cfg.ImportDir != "" {
		if err := app.importJSONNotes(context.Background(), cfg.ImportDir); err != nil {
			db.Close()
			return nil, err
		}
	}
	return app, nil
}

func (a *App) Close() error {
	return a.db.Close()
}

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

func (a *App) loadTemplates() error {
	funcs := template.FuncMap{
		"staticURL": func(name string) string {
			return "/static/" + strings.TrimLeft(name, "/")
		},
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

func (a *App) loadInitContent() error {
	b, err := os.ReadFile(filepath.Join("templates", "init_note.md"))
	if err != nil {
		return fmt.Errorf("read init note template: %w", err)
	}
	a.initContent = string(b)
	return nil
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("POST /notes", a.requireAuth(a.handleCreateNote))
	mux.HandleFunc("GET /notes/{slug}", a.handleViewNote)
	mux.HandleFunc("POST /notes/{slug}", a.handleUpdateNote)
	mux.HandleFunc("GET /notes/{slug}/meta", a.handleNoteMeta)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.EscapedPath(), "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

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

func validateSlug(slug string) bool {
	return slugPattern.MatchString(slug)
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	a.render(w, http.StatusOK, "index.html", nil)
}

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

	markdown := fmt.Sprintf("# %s\n%s", slug, a.initContent)
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
	http.Redirect(w, r, "/notes/"+slug, http.StatusSeeOther)
}

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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":    note.Version,
		"updated_at": note.UpdatedAt,
	})
}

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
	if len(markdown) > maxMarkdownSize {
		http.Error(w, "Note content too large", http.StatusRequestEntityTooLarge)
		return
	}
	clientVersion, err := strconv.Atoi(r.FormValue("version"))
	if err != nil {
		clientVersion = 0
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
	if rows == 1 {
		if err := tx.Commit(); err != nil {
			http.Error(w, "Update note failed", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/notes/"+slug, http.StatusSeeOther)
		return
	}

	var current Note
	err = tx.QueryRowContext(
		r.Context(),
		`SELECT slug, markdown, version, created_at, updated_at FROM notes WHERE slug = ?`,
		slug,
	).Scan(&current.Slug, &current.Markdown, &current.Version, &current.CreatedAt, &current.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Load note failed", http.StatusInternalServerError)
		return
	}
	a.render(w, http.StatusConflict, "conflict.html", map[string]any{
		"Slug":           slug,
		"MyMarkdown":     markdown,
		"TheirMarkdown":  current.Markdown,
		"CurrentVersion": current.Version,
	})
}

func (a *App) getNote(ctx context.Context, slug string) (Note, bool, error) {
	var note Note
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

func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

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

func (a *App) importJSONNotes(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read import directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), ".json")
		if !validateSlug(slug) {
			return fmt.Errorf("invalid note filename %q", entry.Name())
		}
		path := filepath.Join(dir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read note %s: %w", entry.Name(), err)
		}
		var imported noteJSON
		if err := json.Unmarshal(b, &imported); err != nil {
			return fmt.Errorf("parse note %s: %w", entry.Name(), err)
		}
		if imported.Version <= 0 {
			return fmt.Errorf("note %s has invalid version %d", entry.Name(), imported.Version)
		}
		if len(imported.Markdown) > maxMarkdownSize {
			return fmt.Errorf("note %s exceeds max markdown size", entry.Name())
		}
		_, err = a.db.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO notes (slug, markdown, version) VALUES (?, ?, ?)`,
			slug,
			imported.Markdown,
			imported.Version,
		)
		if err != nil {
			return fmt.Errorf("import note %s: %w", entry.Name(), err)
		}
	}
	return nil
}
