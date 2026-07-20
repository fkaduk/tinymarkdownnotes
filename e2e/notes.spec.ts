import { expect, test, type Page } from "@playwright/test";

function uniqueSlug(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

async function createNote(page: Page, slug: string): Promise<void> {
  await page.goto("/");
  await page.getByPlaceholder("note-name").fill(slug);
  await page.getByRole("button", { name: "Open Note" }).click();

  await expect(page).toHaveURL(`/notes/${slug}`);
  await expect(page.locator("#version-field")).toHaveValue("1");
}

test("creates a note and renders its initial preview", async ({ page }) => {
  const slug = uniqueSlug("create");

  await createNote(page, slug);

  await expect(page).toHaveTitle(`Note: ${slug}`);
  await expect(page.locator("#preview")).toContainText(slug);
});

test("opens the existing note for a duplicate slug", async ({ page }) => {
  const slug = uniqueSlug("duplicate");
  await createNote(page, slug);

  await page.goto("/");
  await page.getByPlaceholder("note-name").fill(slug);
  await page.getByRole("button", { name: "Open Note" }).click();

  await expect(page).toHaveURL(`/notes/${slug}`);
  await expect(page.locator("#version-field")).toHaveValue("1");
});

test("edits and reloads a note with its next version", async ({ page }) => {
  const slug = uniqueSlug("edit");
  const markdown = "# Updated in Playwright\n\n- [x] Saved";
  await createNote(page, slug);

  await page.getByRole("button", { name: "Edit" }).click();
  await page.locator("#editor").fill(markdown);
  const updateResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/notes/${slug}`),
  );
  await page.getByRole("button", { name: "Save" }).click();

  expect((await updateResponse).status()).toBe(303);
  await expect(page.locator("#version-field")).toHaveValue("2");
  await page.getByRole("button", { name: "Edit" }).click();
  await expect(page.locator("#editor")).toHaveValue(markdown);
});

test("rejects a stale save without discarding its draft", async ({
  context,
  page,
}) => {
  const slug = uniqueSlug("conflict");
  const firstEdit = "# Saved by the first tab";
  const staleDraft = "# Unsaved draft from the second tab";
  await createNote(page, slug);

  const secondPage = await context.newPage();
  await secondPage.goto(`/notes/${slug}`);

  await page.getByRole("button", { name: "Edit" }).click();
  await secondPage.getByRole("button", { name: "Edit" }).click();
  await page.locator("#editor").fill(firstEdit);
  await secondPage.locator("#editor").fill(staleDraft);

  const firstUpdateResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/notes/${slug}`),
  );
  await page.getByRole("button", { name: "Save" }).click();
  expect((await firstUpdateResponse).status()).toBe(303);
  await expect(page.locator("#version-field")).toHaveValue("2");

  const conflictResponse = secondPage.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/notes/${slug}`),
  );
  await secondPage.getByRole("button", { name: "Save" }).click();

  expect((await conflictResponse).status()).toBe(409);
  await expect(secondPage.locator("#stale-warning")).toBeVisible();
  await expect(secondPage.locator("#stale-warning")).toContainText(
    "Copy your changes from the editor somewhere safe, then reload",
  );
  await expect(secondPage.locator("#editor")).toHaveValue(staleDraft);
  await expect(secondPage.getByRole("button", { name: "Save" })).toBeDisabled();
});
