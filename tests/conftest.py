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

    from app import create_app

    # Create app with test configuration
    test_app = create_app(
        test_config={
            "TESTING": True,
            "NOTES_DIR": Path(test_notes_dir),
            "ADMIN_KEY": "test-admin-key",
        }
    )

    yield test_app

    # Cleanup
    shutil.rmtree(test_notes_dir, ignore_errors=True)
