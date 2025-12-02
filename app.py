import json
import os
import re
from pathlib import Path

from flask import Flask, redirect, render_template, request, url_for
from flask_httpauth import HTTPBasicAuth

SLUG_PATTERN = re.compile(r"^[a-zA-Z0-9_-]{1,64}$")
MAX_MARKDOWN_SIZE = 100_000


def create_app(config=None):
    """Application factory for Flask app."""
    app = Flask(__name__)

    app.config.from_mapping(
        NOTES_DIR=Path("notes"),
        ADMIN_KEY=os.environ.get("NOTES_ADMIN_KEY", "change-me-in-production"),
    )
    if config is not None:
        app.config.from_mapping(config)

    notes_dir = app.config["NOTES_DIR"]
    notes_dir.mkdir(exist_ok=True)

    auth = HTTPBasicAuth()

    @auth.verify_password
    def verify_password(username, password):
        """Verify password for HTTP Basic Auth. Username is ignored."""
        if password == app.config["ADMIN_KEY"]:
            return "admin"
        return None

    def validate_slug(slug):
        """Validate slug against allowed pattern."""
        return SLUG_PATTERN.match(slug) is not None

    def get_note_path(slug):
        """Get the file path for a note."""
        return app.config["NOTES_DIR"] / f"{slug}.json"

    def load_note(slug):
        """Load a note from disk. Returns None if not found."""
        note_path = get_note_path(slug)
        if not note_path.exists():
            return None
        with open(note_path, "r") as f:
            return json.load(f)

    def save_note(slug, markdown, version):
        """Save a note to disk."""
        note_path = get_note_path(slug)
        note_data = {
            "markdown": markdown,
            "version": version,
        }
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

    @app.route("/notes", methods=["POST"])
    @auth.login_required
    def create_note():
        """Create a new note."""
        slug = request.form.get("slug", "").strip()

        if not validate_slug(slug):
            return """
            <script>
                alert('Invalid note name');
                window.history.back();
            </script>
            """, 400

        if load_note(slug):
            return """
            <script>
                alert('Note already exists');
                window.history.back();
            </script>
            """, 409

        init_template_path = Path(__file__).parent / "templates" / "init_note.md"
        with open(init_template_path, "r") as f:
            template_content = f.read()
        default_markdown = f"# {slug}\n{template_content}"
        save_note(slug, default_markdown, 1)

        return redirect(url_for("view_note", slug=slug), code=303)

    @app.route("/notes/<slug>", methods=["GET"])
    def view_note(slug):
        """View a note."""
        if not validate_slug(slug):
            return "Invalid note slug", 400
        note = load_note(slug)
        if note is None:
            return "Note not found", 404
        return render_template("note.html", slug=slug, note=note)

    @app.route("/notes/<slug>", methods=["POST"])
    def update_note(slug):
        """Update an existing note."""
        if not validate_slug(slug):
            return "Invalid note slug", 400
        note = load_note(slug)
        if note is None:
            return "Note not found", 404
        new_markdown = request.form.get("markdown", "")
        client_version = int(request.form.get("version", 0))
        if len(new_markdown) > MAX_MARKDOWN_SIZE:
            return "Note content too large", 413
        current_version = note["version"]
        if client_version != current_version:
            return """
            <script>
                alert('Conflict: note has been updated by someone else. Please reload the page to see the latest version.');
            </script>
            """, 409
        new_version = current_version + 1
        save_note(slug, new_markdown, new_version)
        return redirect(url_for("view_note", slug=slug), code=303)

    @app.route("/")
    def index():
        """Landing page with create form."""
        return render_template("index.html")

    return app


app = create_app()

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)
