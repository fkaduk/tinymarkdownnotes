import shutil
import sys
import tempfile
from base64 import b64encode
from pathlib import Path

import pytest

# Add project root to Python path
sys.path.insert(0, str(Path(__file__).parent.parent))

from app import create_app


@pytest.fixture
def app():
    """Create test app with isolated temp directory."""
    test_notes_dir = tempfile.mkdtemp()

    test_app = create_app(
        config={
            "TESTING": True,
            "NOTES_DIR": Path(test_notes_dir),
            "ADMIN_KEY": "test-admin-key",
        }
    )
    yield test_app
    shutil.rmtree(test_notes_dir, ignore_errors=True)


@pytest.fixture
def auth_headers():
    """Create HTTP Basic Auth headers for testing."""
    credentials = b64encode(b":test-admin-key").decode("utf-8")
    return {"Authorization": f"Basic {credentials}"}
