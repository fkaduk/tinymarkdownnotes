import json

import pytest


class TestNoteViewing:
    """Test note viewing functionality."""

    def test_view_nonexistent_note_returns_404(self, app):
        """Viewing non-existent note returns 404"""
        response = app.test_client().get("/notes/doesnotexist")
        assert response.status_code == 404
        assert b"not found" in response.data.lower()

    def test_view_existing_note(self, app, create_note):
        """Viewing an existing note works"""
        create_note("mytest")
        response = app.test_client().get("/notes/mytest")
        assert response.status_code == 200
        assert b"mytest" in response.data
        assert b"First item" in response.data

    def test_invalid_slugs(self, app):
        """
        Invalid slugs are rejected through either
        a) Flasks routing or b) slug validation
        """
        invalid_slugs = [
            "test/path",  # invalid route
            "../etc",  # invalid route
            "/notes/.../etc/passwd",  # invalid route
            "test space",  # invalid char
            "test@note",  # invalid char
            "test.note",  # invalid char
            "",  # empty
            "a" * 65,  # overly long
        ]
        for slug in invalid_slugs:
            response = app.test_client().get(f"/notes/{slug}")
            # Either 400 (our validation) or 404 (Flask routing) blocks the request
            assert response.status_code in [400, 404], (
                f"Slug '{slug}' should be blocked (got {response.status_code})"
            )


class TestNoteCreation:
    """Test note creation."""

    def test_create_note_requires_auth(self, app):
        """Create without auth fails"""
        response = app.test_client().post("/notes", data={"slug": "test"})
        assert response.status_code == 401

    def test_create_note_succeeds(self, app, auth_headers):
        """Create with auth succeeds"""
        response = app.test_client().post(
            "/notes",
            data={"slug": "test"},
            headers=auth_headers,
            follow_redirects=False,
        )
        assert response.status_code == 303
        assert response.location == "/notes/test"

    def test_create_invalid_slug_fails(self, app, auth_headers):
        """Invalid slugs are rejected when creating notes"""
        invalid_slugs = [
            "test/path",  # invalid route
            "../etc",  # invalid route
            "/notes/.../etc/passwd",  # invalid route
            "test space",  # invalid char
            "test@note",  # invalid char
            "test.note",  # invalid char
            "",  # empty
            "a" * 65,  # overly long
        ]
        for slug in invalid_slugs:
            response = app.test_client().post(
                "/notes", data={"slug": slug}, headers=auth_headers
            )
            assert response.status_code == 400, (
                f"Slug '{slug}' should be rejected with 400 (got {response.status_code})"
            )

    def test_create_existing_note_fails(self, app, auth_headers, create_note):
        """Creating duplicate returns 409"""
        create_note("existing")
        response = app.test_client().post(
            "/notes", data={"slug": "existing"}, headers=auth_headers
        )
        assert response.status_code == 409


class TestNoteEditing:
    """Test note editing."""

    def test_edit_note_with_correct_version(self, app, create_note):
        """Editing note without auth and correct version succeeds"""
        create_note("edit-test")
        response = app.test_client().post(
            "/notes/edit-test",
            data={"markdown": "# Updated\n\n- [x] Done", "version": "1"},
            follow_redirects=False,
        )
        assert response.status_code == 303
        assert response.location == "/notes/edit-test"
        response = app.test_client().get("/notes/edit-test")
        assert b"Updated" in response.data
        assert b"Done" in response.data

    def test_edit_note_with_wrong_version_returns_409(self, app, create_note):
        """Editing note with wrong version returns conflict"""
        create_note("conflict-test")
        response = app.test_client().post(
            "/notes/conflict-test",
            data={"markdown": "# Should fail", "version": "999"},
        )

        assert response.status_code == 409
        assert b"Conflict" in response.data

    def test_edit_note_increments_version(self, app, create_note):
        """Editing note increments version number"""
        create_note("version-test")
        app.test_client().post(
            "/notes/version-test",
            data={"markdown": "# Version 2", "version": "1"},
        )

        # Try to edit with old version, should fail
        response = app.test_client().post(
            "/notes/version-test",
            data={"markdown": "# Should fail", "version": "1"},
        )
        assert response.status_code == 409

    def test_edit_nonexistent_note_returns_404(self, app):
        """Editing non-existent note returns 404."""
        response = app.test_client().post(
            "/notes/doesnotexist",
            data={"markdown": "# Test", "version": "1"},
        )
        assert response.status_code == 404

    def test_edit_note_too_large_returns_413(self, app, create_note):
        """Editing note with content too large returns 413."""
        create_note("large-test")
        large_content = "x" * 200_000
        response = app.test_client().post(
            "/notes/large-test",
            data={"markdown": large_content, "version": "1"},
        )

        assert response.status_code == 413


class TestNoteRendering:
    """Test note UI/template rendering."""

    def test_note_has_expected_tabs(self, app, create_note):
        """Note page has expected tabs."""
        create_note("ui-test")
        response = app.test_client().get("/notes/ui-test")
        assert response.status_code == 200
        assert b"preview" in response.data.lower()
        assert b"edit" in response.data.lower()

    def test_note_contains_markdown_scripts(self, app, create_note):
        """Note page includes markdown-it scripts."""
        create_note("render-test")
        response = app.test_client().get("/notes/render-test")
        assert b"markdown-it" in response.data
        assert b"markdown-it-task-lists" in response.data
        assert b"dompurify" in response.data

    def test_note_json_data_embedded(self, app, create_note):
        """Note data is embedded in page for JS."""
        create_note("json-test")
        response = app.test_client().get("/notes/json-test")
        assert b"First item" in response.data
        assert b'name="version"' in response.data


class TestHomePage:
    """Test home page."""

    def test_home_page_renders(self, app):
        """Home page renders successfully."""
        response = app.test_client().get("/")
        assert response.status_code == 200
        assert b"Tiny Markdown Notes" in response.data
