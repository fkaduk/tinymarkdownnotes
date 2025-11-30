import json
import os
import re
from datetime import datetime, timezone
from pathlib import Path

from flask import Flask, jsonify, redirect, render_template, request, url_for

SLUG_PATTERN = re.compile(r"^[a-zA-Z0-9_-]{1,64}$")
MAX_MARKDOWN_SIZE = 100_000


def create_app(test_config=None):
    """Application factory for Flask app."""
    app = Flask(__name__)

    app.config.from_mapping(
        NOTES_DIR=Path("notes"),
        ADMIN_KEY=os.environ.get("NOTES_ADMIN_KEY", "change-me-in-production"),
    )
    if test_config is not None:
        app.config.from_mapping(test_config)

    notes_dir = app.config["NOTES_DIR"]
    notes_dir.mkdir(exist_ok=True)

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
            "created_at": datetime.now(timezone.utc).isoformat()
            if not note_path.exists()
            else None,
        }
        if note_path.exists():
            existing = load_note(slug)
            note_data["created_at"] = existing.get("created_at")
        with open(note_path, "w") as f:
            json.dump(note_data, f, indent=2)

    def notify_change(slug, request_info):
        """Log note changes. Extend this for email/webhook notifications."""
        ip = request_info.get("ip", "unknown")
        user_agent = request_info.get("user_agent", "unknown")
        url = request_info.get("url", "unknown")
        print(f"[CHANGE] Note '{slug}' updated from {ip} | {user_agent} | {url}")

    @app.route("/notes/<slug>", methods=["GET"])
    def view_note(slug):
        """View or create a note."""
        if not validate_slug(slug):
            return "Invalid note slug", 400
        note = load_note(slug)
        if note is None:
            admin_key = request.args.get("key")
            if admin_key == app.config["ADMIN_KEY"]:
                # Create new note
                default_markdown = f"# {slug}\n\n- [ ] First item\n"
                save_note(slug, default_markdown, 1)
                note = load_note(slug)
            else:
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
            return render_template("conflict.html", slug=slug), 409
        new_version = current_version + 1
        save_note(slug, new_markdown, new_version)
        notify_change(
            slug,
            {
                "ip": request.remote_addr,
                "user_agent": request.headers.get("User-Agent"),
                "url": request.url,
            },
        )
        return redirect(url_for("view_note", slug=slug))

    @app.route("/")
    def index():
        """Simple landing page."""
        return """
        <html>
        <head><title>Tiny Markdown Notes</title></head>
        <body>
            <h1>Tiny Markdown Notes</h1>
            <p>Go to <code>/notes/&lt;slug&gt;?key=ADMIN_KEY</code> to create a new note.</p>
            <p>Example: <a href="/notes/test?key=change-me-in-production">/notes/test?key=change-me-in-production</a></p>
        </body>
        </html>
        """

    return app


if __name__ == "__main__":
    app = create_app()
    app.run(host="0.0.0.0", port=5000, debug=True)
