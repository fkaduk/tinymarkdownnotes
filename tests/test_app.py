import json

import pytest


class TestNoteCreation:
    """Test note creation functionality."""

    def test_create_note_without_auth_returns_404(self, app):
        """Creating note without auth fails"""
        response = app.test_client().get("/notes/test")
        assert response.status_code == 404

    def test_create_note_with_auth_succeeds(self, app, auth_headers):
        """Creating note with auth succeeds"""
        response = app.test_client().get("/notes/test", headers=auth_headers)
        assert response.status_code == 200
        assert b"Note: test" in response.data
        assert b"First item" in response.data

    def test_invalid_slug_blocked(self, app, auth_headers):
        """Invalid slug characters are blocked"""
        response = app.test_client().get("/notes/../etc/passwd", headers=auth_headers)
        # Either 400 (our validation) or 404 (Flask routing) is acceptable
        assert response.status_code in [400, 404]

    def test_long_slug_blocked(self, app, auth_headers):
        """Overly long slug is blocked"""
        long_slug = "a" * 65
        response = app.test_client().get(f"/notes/{long_slug}", headers=auth_headers)
        assert response.status_code == 400


class TestNoteViewing:
    """Test note viewing functionality."""

    def test_view_nonexistend_note_fails(self, app):
        """Accessing non-existent note without key returns 404"""
        response = app.test_client().get("/notes/test")
        assert response.status_code == 404
        assert b"not found" in response.data.lower()

    def test_view_existing_note(self, app):
        """Viewing an existing note works."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "mytest.json"
        note_data = {
            "markdown": "# mytest\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        # View note
        response = app.test_client().get("/notes/mytest")
        assert response.status_code == 200
        assert b"Note: mytest" in response.data
        assert b"First item" in response.data

    def test_note_has_preview_and_edit_tabs(self, app):
        """Note page has Preview and Edit tabs."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "ui-test.json"
        note_data = {
            "markdown": "# ui-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        response = app.test_client().get("/notes/ui-test")

        assert response.status_code == 200
        assert b"Preview" in response.data
        assert b"Edit" in response.data


class TestNoteEditing:
    """Test note editing functionality."""

    def test_edit_note_with_correct_version(self, app, auth_headers):
        """Editing note with correct version succeeds."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "edit-test.json"
        note_data = {
            "markdown": "# edit-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        # Edit note
        response = app.test_client().post(
            "/notes/edit-test",
            data={"markdown": "# Updated\n\n- [x] Done", "version": "1"},
            headers=auth_headers,
            follow_redirects=False,
        )

        assert response.status_code == 302  # Redirect
        assert response.location == "/notes/edit-test"

        # Verify edit
        response = app.test_client().get("/notes/edit-test")
        assert b"Updated" in response.data
        assert b"Done" in response.data

    def test_edit_note_with_wrong_version_returns_409(self, app, auth_headers):
        """Editing note with wrong version returns conflict."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "conflict-test.json"
        note_data = {
            "markdown": "# conflict-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        # Try to edit with wrong version
        response = app.test_client().post(
            "/notes/conflict-test",
            data={"markdown": "# Should fail", "version": "999"},
            headers=auth_headers,
        )

        assert response.status_code == 409
        assert b"Conflict" in response.data

    def test_edit_note_increments_version(self, app, auth_headers):
        """Editing note increments version number."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "version-test.json"
        note_data = {
            "markdown": "# version-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        # Edit note
        app.test_client().post(
            "/notes/version-test",
            data={"markdown": "# Version 2", "version": "1"},
            headers=auth_headers,
        )

        # Check version in file
        note_path = app.config["NOTES_DIR"] / "version-test.json"
        with open(note_path) as f:
            data = json.load(f)
            assert data["version"] == 2

    def test_edit_nonexistent_note_returns_404(self, app, auth_headers):
        """Editing non-existent note returns 404."""
        response = app.test_client().post(
            "/notes/doesnotexist",
            data={"markdown": "# Test", "version": "1"},
            headers=auth_headers,
        )
        assert response.status_code == 404

    def test_edit_note_too_large_returns_413(self, app, auth_headers):
        """Editing note with content too large returns 413."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "large-test.json"
        note_data = {
            "markdown": "# large-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        # Try to save huge content
        large_content = "x" * 200_000
        response = app.test_client().post(
            "/notes/large-test",
            data={"markdown": large_content, "version": "1"},
            headers=auth_headers,
        )

        assert response.status_code == 413


class TestSlugValidation:
    """Test slug validation."""

    def test_invalid_slugs(self, app, auth_headers):
        """
        Invalid slugs are rejected through either
        a) Flasks routing or b) slug validation
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
            response = app.test_client().get(f"/notes/{slug}", headers=auth_headers)
            # Either 400 (our validation) or 404 (Flask routing) blocks the request
            assert response.status_code in [400, 404], (
                f"Slug '{slug}' should be blocked (got {response.status_code})"
            )


class TestMarkdownRendering:
    """Test markdown rendering."""

    def test_note_contains_markdown_scripts(self, app):
        """Note page includes markdown-it scripts."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "render-test.json"
        note_data = {
            "markdown": "# render-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

        response = app.test_client().get("/notes/render-test")

        assert b"markdown-it" in response.data
        assert b"markdown-it-task-lists" in response.data
        assert b"dompurify" in response.data

    def test_note_json_data_embedded(self, app):
        """Note data is embedded in page for JS."""
        from datetime import datetime, timezone

        # Create note manually
        note_path = app.config["NOTES_DIR"] / "json-test.json"
        note_data = {
            "markdown": "# json-test\n\n- [ ] First item\n",
            "version": 1,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

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
