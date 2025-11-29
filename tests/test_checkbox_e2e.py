"""
End-to-end tests for checkbox functionality using Playwright.

These tests require the app to be running. Run with:
    pytest tests/test_checkbox_e2e.py --headed  # to see browser
    pytest tests/test_checkbox_e2e.py           # headless mode
"""
import pytest
import json
from pathlib import Path


@pytest.fixture(scope="session")
def browser_context_args(browser_context_args):
    """Configure browser context."""
    return {
        **browser_context_args,
        "viewport": {"width": 1280, "height": 720},
    }


@pytest.mark.e2e
class TestCheckboxInteraction:
    """Test checkbox interaction in the browser."""

    @pytest.fixture(autouse=True)
    def setup(self, page):
        """Setup for each test."""
        self.page = page
        self.base_url = "http://localhost:5000"

    def test_checkbox_toggle_updates_markdown(self):
        """Clicking checkbox in preview mode updates markdown and saves."""
        # Create a note with checkboxes
        slug = "checkbox-test"
        self.page.goto(f"{self.base_url}/notes/{slug}?key=change-me-in-production")

        # Wait for page to load
        self.page.wait_for_selector("#preview")

        # Should be in preview mode by default
        assert self.page.locator("#preview").is_visible()
        assert not self.page.locator("#editor-wrap").is_visible()

        # Find the first checkbox
        checkbox = self.page.locator('input[type="checkbox"]').first
        assert not checkbox.is_checked()

        # Click the checkbox
        checkbox.click()

        # Page should reload after auto-save
        self.page.wait_for_load_state("networkidle")

        # Checkbox should now be checked
        checkbox = self.page.locator('input[type="checkbox"]').first
        assert checkbox.is_checked()

        # Switch to edit mode to verify markdown
        self.page.click("#edit-tab")
        self.page.wait_for_selector("#editor")

        editor_content = self.page.locator("#editor").input_value()
        assert "[x]" in editor_content.lower()

    def test_multiple_checkboxes_toggle_correctly(self):
        """Multiple checkboxes can be toggled independently."""
        slug = "multi-checkbox-test"

        # Create note with custom markdown
        self.page.goto(f"{self.base_url}/notes/{slug}?key=change-me-in-production")
        self.page.wait_for_selector("#edit-tab")

        # Switch to edit mode
        self.page.click("#edit-tab")
        self.page.wait_for_selector("#editor")

        # Add multiple checkboxes
        markdown = """# Todo List

- [ ] First task
- [ ] Second task
- [ ] Third task
"""
        self.page.locator("#editor").fill(markdown)
        self.page.click("button[type='submit']")

        # Wait for page to reload
        self.page.wait_for_load_state("networkidle")

        # Should be in preview mode
        checkboxes = self.page.locator('input[type="checkbox"]').all()
        assert len(checkboxes) == 3

        # All should be unchecked
        for cb in checkboxes:
            assert not cb.is_checked()

        # Toggle second checkbox
        checkboxes[1].click()
        self.page.wait_for_load_state("networkidle")

        # Check results
        checkboxes = self.page.locator('input[type="checkbox"]').all()
        assert not checkboxes[0].is_checked()
        assert checkboxes[1].is_checked()
        assert not checkboxes[2].is_checked()

    def test_preview_edit_toggle_works(self):
        """Preview and Edit tabs toggle correctly."""
        slug = "tab-test"
        self.page.goto(f"{self.base_url}/notes/{slug}?key=change-me-in-production")

        # Should start in preview mode
        assert self.page.locator("#preview").is_visible()
        assert not self.page.locator("#editor-wrap").is_visible()

        # Click Edit tab
        self.page.click("#edit-tab")

        # Should now be in edit mode
        assert not self.page.locator("#preview").is_visible()
        assert self.page.locator("#editor-wrap").is_visible()

        # Click Preview tab
        self.page.click("#preview-tab")

        # Should be back in preview mode
        assert self.page.locator("#preview").is_visible()
        assert not self.page.locator("#editor-wrap").is_visible()

    def test_edit_and_save_updates_content(self):
        """Editing and saving updates the note content."""
        slug = "edit-save-test"
        self.page.goto(f"{self.base_url}/notes/{slug}?key=change-me-in-production")

        # Switch to edit mode
        self.page.click("#edit-tab")

        # Update content
        new_content = "# Updated Note\n\n- [x] Completed task\n- [ ] Pending task"
        self.page.locator("#editor").fill(new_content)

        # Save
        self.page.click("button[type='submit']")
        self.page.wait_for_load_state("networkidle")

        # Verify content in preview
        preview = self.page.locator("#preview")
        assert "Updated Note" in preview.inner_text()
        assert "Completed task" in preview.inner_text()
        assert "Pending task" in preview.inner_text()

        # Verify checkboxes
        checkboxes = self.page.locator('input[type="checkbox"]').all()
        assert checkboxes[0].is_checked()
        assert not checkboxes[1].is_checked()


# Pytest-playwright fixtures
pytest_plugins = ("pytest_playwright",)


def pytest_configure(config):
    """Register e2e marker."""
    config.addinivalue_line(
        "markers", "e2e: mark test as end-to-end test requiring running server"
    )
