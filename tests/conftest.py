import os
import shutil
import sys
import tempfile
from pathlib import Path

import pytest

# Add project root to Python path
sys.path.insert(0, str(Path(__file__).parent.parent))


@pytest.fixture
def app():
    """Create test app with isolated temp directory."""
    test_notes_dir = tempfile.mkdtemp()
    os.environ["NOTES_ADMIN_KEY"] = "test-admin-key"
    import app as app_module

    app_module.NOTES_DIR = Path(test_notes_dir)
    app_module.ADMIN_KEY = "test-admin-key"
    app_module.app.config["TESTING"] = True
    yield app_module.app
    shutil.rmtree(test_notes_dir, ignore_errors=True)
