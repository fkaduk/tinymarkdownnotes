import json

import pytest


class TestNoteCreation:
    """Test note creation functionality."""

    def test_create_note_without_key_returns_404(self, app):
        """Accessing non-existent note without key returns 404."""
        response = app.test_client().get("/notes/test")
        assert response.status_code == 404
        assert b"Note not found" in response.data

    def test_create_note_with_wrong_key_returns_404(self, app):
        """Creating note with wrong admin key returns 404."""
        response = app.test_client().get("/notes/test?key=wrong-key")
        assert response.status_code == 404

    def test_create_note_with_correct_key_succeeds(self, app):
        """Creating note with correct admin key succeeds."""
        response = app.test_client().get("/notes/test?key=test-admin-key")
        assert response.status_code == 200
        assert b"Note: test" in response.data
        assert b"First item" in response.data

    def test_invalid_slug_blocked(self, app):
        """Invalid slug characters are blocked by Flask routing or validation.

        Path traversal attempts like '../etc/passwd' return 404 because Flask's
        routing doesn't match them to our /notes/<slug> pattern. This is fine
        from a security perspective - the malicious request is blocked before
        reaching our handler.

        Other invalid characters that do reach our handler (like 'test@note')
        would return 400 from our slug validation, but Flask's routing provides
        the first line of defense.
        """
        response = app.test_client().get("/notes/../etc/passwd?key=test-admin-key")
        # Either 400 (our validation) or 404 (Flask routing) is acceptable
        assert response.status_code in [400, 404]

    def test_slug_too_long_returns_400(self, app):
        """Slug longer than 64 chars returns 400."""
        long_slug = "a" * 65
        response = app.test_client().get(f"/notes/{long_slug}?key=test-admin-key")
        assert response.status_code == 400


class TestNoteViewing:
    """Test note viewing functionality."""

    def test_view_existing_note(self, app):
        """Viewing an existing note works."""
        # Create note
        app.test_client().get("/notes/mytest?key=test-admin-key")

        # View note
        response = app.test_client().get("/notes/mytest")
        assert response.status_code == 200
        assert b"Note: mytest" in response.data
        assert b"First item" in response.data

    def test_view_note_includes_share_link(self, app):
        """Note page includes share link."""
        app.test_client().get("/notes/share-test?key=test-admin-key")
        response = app.test_client().get("/notes/share-test")

        assert response.status_code == 200
        assert b"Share this link" in response.data

    def test_note_has_preview_and_edit_tabs(self, app):
        """Note page has Preview and Edit tabs."""
        app.test_client().get("/notes/ui-test?key=test-admin-key")
        response = app.test_client().get("/notes/ui-test")

        assert response.status_code == 200
        assert b"Preview" in response.data
        assert b"Edit" in response.data


class TestNoteEditing:
    """Test note editing functionality."""

    def test_edit_note_with_correct_version(self, app):
        """Editing note with correct version succeeds."""
        # Create note
        app.test_client().get("/notes/edit-test?key=test-admin-key")

        # Edit note
        response = app.post(
            "/notes/edit-test",
            data={"markdown": "# Updated\n\n- [x] Done", "version": "1"},
            follow_redirects=False,
        )

        assert response.status_code == 302  # Redirect
        assert response.location == "/notes/edit-test"

        # Verify edit
        response = app.test_client().get("/notes/edit-test")
        assert b"Updated" in response.data
        assert b"Done" in response.data

    def test_edit_note_with_wrong_version_returns_409(self, app):
        """Editing note with wrong version returns conflict."""
        # Create note
        app.test_client().get("/notes/conflict-test?key=test-admin-key")

        # Try to edit with wrong version
        response = app.post(
            "/notes/conflict-test", data={"markdown": "# Should fail", "version": "999"}
        )

        assert response.status_code == 409
        assert b"Conflict" in response.data

    def test_edit_note_increments_version(self, app):
        """Editing note increments version number."""
        from pathlib import Path

        import app as app_module

        # Create note
        app.test_client().get("/notes/version-test?key=test-admin-key")

        # Edit note
        app.post(
            "/notes/version-test", data={"markdown": "# Version 2", "version": "1"}
        )

        # Check version in file
        note_path = app_module.NOTES_DIR / "version-test.json"
        with open(note_path) as f:
            data = json.load(f)
            assert data["version"] == 2

    def test_edit_nonexistent_note_returns_404(self, app):
        """Editing non-existent note returns 404."""
        response = app.post(
            "/notes/doesnotexist", data={"markdown": "# Test", "version": "1"}
        )
        assert response.status_code == 404

    def test_edit_note_too_large_returns_413(self, app):
        """Editing note with content too large returns 413."""
        # Create note
        app.test_client().get("/notes/large-test?key=test-admin-key")

        # Try to save huge content
        large_content = "x" * 200_000
        response = app.post(
            "/notes/large-test", data={"markdown": large_content, "version": "1"}
        )

        assert response.status_code == 413


class TestSlugValidation:
    """Test slug validation."""

    def test_valid_slugs(self, app):
        """Valid slugs are accepted."""
        valid_slugs = ["test", "test-123", "my_note", "ABC-def_123"]

        for slug in valid_slugs:
            response = app.test_client().get(f"/notes/{slug}?key=test-admin-key")
            assert response.status_code == 200, f"Slug '{slug}' should be valid"

    def test_invalid_slugs(self, app):
        """Invalid slugs are rejected.

        Invalid slugs are blocked either by:
        1. Flask's routing (404) - for paths with '/', '..' etc that don't match
           the route pattern /notes/<slug>
        2. Our slug validation (400) - for characters that reach our handler but
           fail the regex ^[a-zA-Z0-9_-]{1,64}$

        Both outcomes are security-wise equivalent - the invalid slug is blocked.
        """
        invalid_slugs = [
            "test/path",  # Flask routing: 404
            "../etc",  # Flask routing: 404
            "test space",  # Our validation: 400
            "test@note",  # Our validation: 400
            "test.note",  # Our validation: 400
            "",  # Flask routing: 404
        ]

        for slug in invalid_slugs:
            response = app.test_client().get(f"/notes/{slug}?key=test-admin-key")
            # Either 400 (our validation) or 404 (Flask routing) blocks the request
            assert response.status_code in [400, 404], (
                f"Slug '{slug}' should be blocked (got {response.status_code})"
            )


class TestMarkdownRendering:
    """Test markdown rendering."""

    def test_note_contains_markdown_scripts(self, app):
        """Note page includes markdown-it scripts."""
        app.test_client().get("/notes/render-test?key=test-admin-key")
        response = app.test_client().get("/notes/render-test")

        assert b"markdown-it" in response.data
        assert b"markdown-it-task-lists" in response.data
        assert b"dompurify" in response.data

    def test_note_json_data_embedded(self, app):
        """Note data is embedded in page for JS."""
        app.test_client().get("/notes/json-test?key=test-admin-key")
        response = app.test_client().get("/notes/json-test")

        # Check that markdown content is in the page
        assert b"First item" in response.data
        # Check version is in hidden field
        assert b'value="1"' in response.data


class TestHomePage:
    """Test home page."""

    def test_home_page_renders(self, app):
        """Home page renders successfully."""
        response = app.test_client().get("/")
        assert response.status_code == 200
        assert b"Tiny Markdown Notes" in response.data
