import os
import sys
import tempfile
import shutil
import pytest
from pathlib import Path

# Add project root to Python path
sys.path.insert(0, str(Path(__file__).parent.parent))


@pytest.fixture(scope="function")
def app():
    """Create and configure a test app instance.

    Uses function scope so each test gets a fresh app with isolated state.
    """
    # Create a temporary directory for test notes
    test_notes_dir = tempfile.mkdtemp()

    # Set environment variables before importing app
    os.environ['NOTES_ADMIN_KEY'] = 'test-admin-key'

    # Import app module
    import app as app_module

    # Override notes directory for testing
    app_module.NOTES_DIR = Path(test_notes_dir)
    app_module.ADMIN_KEY = 'test-admin-key'

    # Configure app for testing
    app_module.app.config.update({
        'TESTING': True,
        'DEBUG': False,
    })

    yield app_module.app

    # Cleanup
    shutil.rmtree(test_notes_dir, ignore_errors=True)


@pytest.fixture
def client(app):
    """Test client for the app with application context."""
    with app.test_client() as client:
        with app.app_context():
            yield client


@pytest.fixture
def runner(app):
    """CLI test runner for the app."""
    return app.test_cli_runner()
