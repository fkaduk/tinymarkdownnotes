package main

// Tests use Go's standard testing and httptest packages. Because this file is in
// package main (rather than main_test), it can also exercise unexported helpers
// such as getNote and validateSlug.
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// newTestApp creates an isolated application for each test. t.TempDir returns a
// unique temporary directory and removes it automatically after the test, so a
// test never reads or modifies the real notes database.
func newTestApp(t *testing.T) *App {
	// Helper marks this as test support code. If it calls t.Fatal, Go reports the
	// caller's line as the failure location instead of a line inside this helper.
	t.Helper()
	app, err := NewApp(t.TempDir()+"/notes.db", "test-admin-key")
	if err != nil {
		t.Fatal(err)
	}
	// t.Cleanup is the test equivalent of defer, but registered cleanup also runs
	// when a helper returns control to its caller.
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return app
}

// createTestNote inserts fixture data directly. Tests for viewing and updating
// do not need to repeat the separate HTTP creation flow in their setup.
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

// authHeader builds the Basic Authentication header expected by requireAuth.
// Basic Auth encodes "username:password" with base64; it is encoding, not
// encryption, which is why a deployed app must be served over HTTPS.
func authHeader() string {
	credentials := base64.StdEncoding.EncodeToString([]byte(":test-admin-key"))
	return "Basic " + credentials
}

// formRequest sends an in-memory form request through the real router. Recorder
// captures the status, headers, and body without opening a network port.
func formRequest(app *App, method, path string, values url.Values, auth bool) *httptest.ResponseRecorder {
	// url.Values.Encode creates an application/x-www-form-urlencoded request body.
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if auth {
		req.Header.Set("Authorization", authHeader())
	}
	rr := httptest.NewRecorder()
	// Calling ServeHTTP directly is fast and deterministic while still exercising
	// routing, middleware, handlers, templates, and the database together.
	app.Routes().ServeHTTP(rr, req)
	return rr
}

// The first tests cover ordinary HTTP behavior: missing resources, successful
// rendering, validation, authentication, and redirects.
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

	// A table-driven loop applies the same assertion to several inputs. This is a
	// common Go testing style and makes adding another case inexpensive.
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

// A mismatched version must produce 409 Conflict instead of overwriting a newer
// edit. Client-side JavaScript improves the UX, but this server test proves the
// data-protection rule does not depend on JavaScript.
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

// Request size limits are tested at the HTTP boundary where callers observe
// them. 413 is the standard status for a payload the server refuses as too large.
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

// The meta endpoint is intentionally small JSON used by browser polling.
func TestNoteMetaReturnsLatestVersion(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "meta-test", "", 4)

	req := httptest.NewRequest(http.MethodGet, "/notes/meta-test/meta", nil)
	rr := httptest.NewRecorder()
	app.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	// An anonymous struct is useful when a test only needs one field and does not
	// warrant a reusable named type.
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

// This test verifies both halves of a rejected save: the HTTP response reports a
// conflict and the persisted row remains unchanged.
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

// TestConcurrentSameVersionSavesOnlyOne exercises the race the version column is
// designed to prevent. Both goroutines begin with the same version, but the
// atomic SQL predicate permits exactly one update.
func TestConcurrentSameVersionSavesOnlyOne(t *testing.T) {
	app := newTestApp(t)
	createTestNote(t, app, "race-test", "# Original\n", 1)
	handler := app.Routes()

	// WaitGroup waits for both goroutines. The buffered channel lets each one send
	// a status without blocking while the main test goroutine is waiting.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, markdown := range []string{"# First\n", "# Second\n"} {
		wg.Add(1)
		// Passing markdown as an argument gives each goroutine its own value and
		// makes the intended capture explicit.
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
	// A channel is closed by its sender once no more values can arrive. Closing it
	// lets the range loop below terminate naturally.
	close(statuses)

	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusSeeOther] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("statuses = %#v, want one 303 and one 409", counts)
	}
}
