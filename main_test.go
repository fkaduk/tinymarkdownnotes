package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := NewApp(Config{
		DBPath:   t.TempDir() + "/notes.db",
		AdminKey: "test-admin-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return app
}

func createTestNote(t *testing.T, app *App, slug, markdown string, version int) {
	t.Helper()
	if markdown == "" {
		markdown = "# " + slug + "\n\n- [ ] First item\n"
	}
	_, err := app.db.Exec(
		`INSERT INTO notes (slug, markdown, version) VALUES (?, ?, ?)`,
		slug,
		markdown,
		version,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func authHeader() string {
	credentials := base64.StdEncoding.EncodeToString([]byte(":test-admin-key"))
	return "Basic " + credentials
}

func formRequest(app *App, method, path string, values url.Values, auth bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if auth {
		req.Header.Set("Authorization", authHeader())
	}
	rr := httptest.NewRecorder()
	app.Routes().ServeHTTP(rr, req)
	return rr
}

func TestViewNonexistentNoteReturns404(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/notes/doesnotexist", nil)
	rr := httptest.NewRecorder()
	app.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "not found") {
		t.Fatalf("response should mention not found: %s", rr.Body.String())
	}
}

func TestViewExistingNote(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "mytest", "", 1)

	req := httptest.NewRequest(http.MethodGet, "/notes/mytest", nil)
	rr := httptest.NewRecorder()
	app.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "mytest") || !strings.Contains(body, "First item") {
		t.Fatalf("response missing note content: %s", body)
	}
}

func TestInvalidSlugsAreRejected(t *testing.T) {
	app := newTestApp(t)
	invalidPaths := []string{
		"/notes/test/path",
		"/notes/../etc",
		"/notes/test%20space",
		"/notes/test@note",
		"/notes/test.note",
		"/notes/" + strings.Repeat("a", 65),
	}

	for _, path := range invalidPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		app.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 400 or 404", path, rr.Code)
		}
	}
}

func TestCreateNoteRequiresAuth(t *testing.T) {
	app := newTestApp(t)
	rr := formRequest(app, http.MethodPost, "/notes", url.Values{"slug": {"test"}}, false)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestCreateNoteSucceeds(t *testing.T) {
	app := newTestApp(t)
	rr := formRequest(app, http.MethodPost, "/notes", url.Values{"slug": {"test"}}, true)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSeeOther)
	}
	if rr.Header().Get("Location") != "/notes/test" {
		t.Fatalf("location = %q", rr.Header().Get("Location"))
	}
}

func TestCreateInvalidSlugFails(t *testing.T) {
	app := newTestApp(t)
	rr := formRequest(app, http.MethodPost, "/notes", url.Values{"slug": {"test space"}}, true)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestCreateDuplicateNoteFails(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "existing", "", 1)

	rr := formRequest(app, http.MethodPost, "/notes", url.Values{"slug": {"existing"}}, true)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusConflict)
	}
}

func TestEditNoteWithCorrectVersion(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "edit-test", "", 1)

	rr := formRequest(app, http.MethodPost, "/notes/edit-test", url.Values{
		"markdown": {"# Updated\n\n- [x] Done"},
		"version":  {"1"},
	}, false)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSeeOther)
	}
	if rr.Header().Get("Location") != "/notes/edit-test" {
		t.Fatalf("location = %q", rr.Header().Get("Location"))
	}
}

func TestEditWithWrongVersionReturnsConflict(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "conflict-test", "", 1)

	rr := formRequest(app, http.MethodPost, "/notes/conflict-test", url.Values{
		"markdown": {"# Should fail"},
		"version":  {"999"},
	}, false)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusConflict)
	}
	if !strings.Contains(rr.Body.String(), "Someone else saved this note first") {
		t.Fatalf("response should explain that the save was rejected: %s", rr.Body.String())
	}
}

func TestEditNoteTooLargeReturns413(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "large-test", "", 1)

	rr := formRequest(app, http.MethodPost, "/notes/large-test", url.Values{
		"markdown": {strings.Repeat("x", 200_000)},
		"version":  {"1"},
	}, false)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestNoteMetaReturnsLatestVersion(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "meta-test", "", 4)

	req := httptest.NewRequest(http.MethodGet, "/notes/meta-test/meta", nil)
	rr := httptest.NewRecorder()
	app.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var payload struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != 4 {
		t.Fatalf("version = %d, want 4", payload.Version)
	}
}

func TestStalePostRejectedEvenWithoutClientGuard(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "stale-test", "# Current\n", 2)

	rr := formRequest(app, http.MethodPost, "/notes/stale-test", url.Values{
		"markdown": {"# Stale draft\n"},
		"version":  {"1"},
	}, false)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusConflict)
	}
	note, ok, err := app.getNote(context.Background(), "stale-test")
	if err != nil || !ok {
		t.Fatalf("get note ok=%v err=%v", ok, err)
	}
	if note.Markdown != "# Current\n" || note.Version != 2 {
		t.Fatalf("stale save changed note: %#v", note)
	}
}

func TestConcurrentSameVersionSavesOnlyOne(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "race-test", "# Original\n", 1)
	handler := app.Routes()

	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, markdown := range []string{"# First\n", "# Second\n"} {
		wg.Add(1)
		go func(markdown string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/notes/race-test", strings.NewReader(url.Values{
				"markdown": {markdown},
				"version":  {"1"},
			}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			statuses <- rr.Code
		}(markdown)
	}
	wg.Wait()
	close(statuses)

	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusSeeOther] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("statuses = %#v, want one 303 and one 409", counts)
	}
}

func TestImportJSONNotes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/imported.json", []byte(`{"markdown":"# Imported\n","version":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{
		DBPath:    t.TempDir() + "/notes.db",
		AdminKey:  "test-admin-key",
		ImportDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	note, ok, err := app.getNote(context.Background(), "imported")
	if err != nil || !ok {
		t.Fatalf("get note ok=%v err=%v", ok, err)
	}
	if note.Markdown != "# Imported\n" || note.Version != 7 {
		t.Fatalf("note = %#v", note)
	}
}
