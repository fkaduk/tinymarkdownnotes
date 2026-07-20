const previewDiv = document.getElementById("preview");
const editorWrap = document.getElementById("editor-wrap");
const editor = document.getElementById("editor");
const versionField = document.getElementById("version-field");
const form = editor.closest("form");
const saveButton = document.getElementById("save-button");
const staleWarning = document.getElementById("stale-warning");
const previewTab = document.getElementById("preview-tab");
const editTab = document.getElementById("edit-tab");
const copyLinkTab = document.getElementById("copy-link-tab");

let currentMarkdown = editor.value;
const loadedVersion = Number(versionField.value);
let isStale = false;

const cleanUrl = window.location.origin + window.location.pathname;

const md = window
  .markdownit({
    html: false,
    breaks: true,
    linkify: true,
  })
  .use(window.markdownitTaskLists, { enabled: true });

function renderPreview() {
  const rawHtml = md.render(currentMarkdown);
  const cleanHtml = DOMPurify.sanitize(rawHtml);

  previewDiv.innerHTML = cleanHtml;

  const checkboxes = previewDiv.querySelectorAll(
    'input[type="checkbox"].task-list-item-checkbox',
  );
  checkboxes.forEach((checkbox, index) => {
    checkbox.addEventListener("click", (event) => {
      toggleCheckbox(index, event.target.checked);
    });
  });
}

function toggleCheckbox(index, checked) {
  if (isStale) {
    updateStaleState(true);
    return;
  }

  const lines = currentMarkdown.split("\n");
  let checkboxCount = 0;

  for (let i = 0; i < lines.length; i++) {
    if (lines[i].match(/^(\s*[-*+])\s+\[[ xX]\]/)) {
      if (checkboxCount === index) {
        lines[i] = lines[i].replace(
          /\[[ xX]\]/,
          checked ? "[x]" : "[ ]",
        );
        break;
      }
      checkboxCount++;
    }
  }

  currentMarkdown = lines.join("\n");
  autoSave();
}

async function saveMarkdown(markdown) {
  const formData = new URLSearchParams();
  formData.set("markdown", markdown);
  formData.set("version", loadedVersion);

  try {
    const response = await fetch(window.location.pathname, {
      method: "POST",
      body: formData,
    });

    if (response.status === 409) {
      updateStaleState(true);
      return;
    }
    if (!response.ok) {
      window.alert("The note could not be saved. Please try again.");
      return;
    }
    window.location.reload();
  } catch (error) {
    console.error("Save failed:", error);
    window.alert("The note could not be saved. Please try again.");
  }
}

async function autoSave() {
  if (isStale) {
    updateStaleState(true);
    return;
  }
  await saveMarkdown(currentMarkdown);
}

function showPreview() {
  if (editorWrap.style.display === "block") {
    currentMarkdown = editor.value;
  }
  renderPreview();
  previewDiv.style.display = "block";
  editorWrap.style.display = "none";
  previewTab.classList.add("active");
  editTab.classList.remove("active");
}

function showEdit() {
  editor.value = currentMarkdown;
  versionField.value = loadedVersion;
  previewDiv.style.display = "none";
  editorWrap.style.display = "block";
  previewTab.classList.remove("active");
  editTab.classList.add("active");
}

function updateStaleState(stale) {
  isStale = stale;
  staleWarning.hidden = !stale;
  saveButton.disabled = stale;
  saveButton.classList.toggle("ghosted", stale);
}

previewTab.addEventListener("click", showPreview);
editTab.addEventListener("click", showEdit);
editor.addEventListener("input", () => {
  currentMarkdown = editor.value;
});
form.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (isStale) {
    return;
  }
  currentMarkdown = editor.value;
  await saveMarkdown(currentMarkdown);
});
copyLinkTab.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(cleanUrl);
    const originalText = copyLinkTab.textContent;
    copyLinkTab.textContent = "✓ Copied";
    setTimeout(() => {
      copyLinkTab.textContent = originalText;
    }, 2000);
  } catch (error) {
    console.error("Failed to copy:", error);
  }
});

renderPreview();
