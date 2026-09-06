"use strict";

const dom = Object.fromEntries([
  "gallery", "gallery-status", "retry-btn", "tag-filters", "search-input", "sort-input",
  "add-modal", "add-form", "add-btn", "upload-preview", "upload-status",
  "preview-modal", "preview-img", "preview-name", "preview-tags", "preview-date",
  "download-btn", "copy-btn", "copy-status",
].map((id) => [id, document.getElementById(id)]));
let memes = [];
let uploadPreviewURL;

function renderGallery() {
  const query = dom["search-input"].value.trim().toLocaleLowerCase();
  const selectedTags = Array.from(dom["tag-filters"].querySelectorAll("input:checked"), (input) => input.value);
  const visibleMemes = memes.filter((meme) => {
    const tags = meme.tags || [];
    return [meme.name, ...tags].some((text) => text.toLocaleLowerCase().includes(query)) &&
      (!selectedTags.length || selectedTags.some((tag) => tags.includes(tag)));
  });
  const sort = dom["sort-input"].value;
  visibleMemes.sort((a, b) => {
    if (sort === "az") return a.name.localeCompare(b.name);
    if (sort === "za") return b.name.localeCompare(a.name);
    const difference = new Date(a.added_at) - new Date(b.added_at);
    return sort === "date-old" ? difference : -difference;
  });
  dom.gallery.replaceChildren(...visibleMemes.map(renderMemeCard));
  dom["gallery-status"].textContent = visibleMemes.length ? "" :
    memes.length ? "no memes match your filters" : "no memes yet. upload one!";
}

function renderMemeCard(meme) {
  const card = document.createElement("button");
  card.type = "button";
  card.className = "card";
  const image = document.createElement("img");
  image.src = meme.image_url;
  image.alt = meme.name;
  image.loading = "lazy";
  const info = document.createElement("span");
  info.className = "card-info";
  for (const [className, text] of [
    ["card-name", meme.name],
    ["card-tags", (meme.tags || []).map((tag) => `#${tag}`).join(" ")],
    ["card-date", new Date(meme.added_at).toLocaleDateString()],
  ]) {
    const line = document.createElement("span");
    line.className = className;
    line.textContent = text;
    info.append(line);
  }
  card.append(image, info);
  card.addEventListener("click", () => openMemePreview(meme));
  return card;
}

function renderTags() {
  const selectedTags = new Set(Array.from(dom["tag-filters"].querySelectorAll("input:checked"), (input) => input.value));
  const tags = [...new Set(memes.flatMap((meme) => meme.tags || []))].sort();
  dom["tag-filters"].replaceChildren(...tags.map((tag) => {
    const label = document.createElement("label");
    const input = document.createElement("input");
    input.type = "checkbox";
    input.value = tag;
    input.checked = selectedTags.has(tag);
    label.append(input, document.createTextNode(tag));
    return label;
  }));
  if (!tags.length) dom["tag-filters"].textContent = "no tags yet";
}

async function loadMemes() {
  dom["retry-btn"].hidden = true;
  try {
    const response = await fetch("/api/memes");
    if (!response.ok) throw new Error(await response.text());
    memes = await response.json();
    renderTags();
    renderGallery();
  } catch (error) {
    dom["gallery-status"].textContent = `could not load memes: ${error.message}`;
    dom["retry-btn"].hidden = false;
  }
}

function showUploadPreview() {
  if (uploadPreviewURL) URL.revokeObjectURL(uploadPreviewURL);
  uploadPreviewURL = undefined;
  dom["upload-preview"].removeAttribute("src");
  const file = dom["add-form"].elements.file.files[0];
  dom["upload-preview"].hidden = !file;
  if (file) {
    uploadPreviewURL = URL.createObjectURL(file);
    dom["upload-preview"].src = uploadPreviewURL;
  }
}

function openAddModal(file) {
  if (dom["add-form"].querySelector('[type="submit"]').disabled) return;
  if (dom["preview-modal"].open) dom["preview-modal"].close();
  if (!dom["add-modal"].open) dom["add-modal"].showModal();
  dom["upload-status"].textContent = "";
  if (file) {
    const transfer = new DataTransfer();
    transfer.items.add(file);
    dom["add-form"].elements.file.files = transfer.files;
    showUploadPreview();
  }
}

function openMemePreview(meme) {
  dom["preview-img"].src = meme.image_url;
  dom["preview-img"].alt = meme.name;
  dom["preview-name"].textContent = meme.name;
  dom["preview-tags"].textContent = (meme.tags || []).map((tag) => `#${tag}`).join(" ");
  dom["preview-date"].textContent = new Date(meme.added_at).toLocaleDateString();
  dom["download-btn"].href = meme.image_url;
  const extension = meme.image_url.split(".").pop().toLowerCase();
  const name = meme.name.replace(/[\\/:*?"<>|\u0000-\u001f]/g, "_");
  dom["download-btn"].download = name.toLowerCase().endsWith(`.${extension}`) ? name : `${name}.${extension}`;
  dom["copy-status"].textContent = "";
  dom["preview-modal"].showModal();
}

dom["add-btn"].addEventListener("click", () => openAddModal());
dom["add-form"].elements.file.addEventListener("change", showUploadPreview);
dom["add-modal"].addEventListener("close", () => {
  dom["add-form"].reset();
  showUploadPreview();
  dom["upload-status"].textContent = "";
});
dom["preview-modal"].addEventListener("close", () => dom["preview-img"].removeAttribute("src"));
for (const button of document.querySelectorAll("[data-close]")) {
  button.addEventListener("click", () => document.getElementById(button.dataset.close).close());
}

dom["add-form"].addEventListener("submit", async (event) => {
  event.preventDefault();
  const submit = dom["add-form"].querySelector('[type="submit"]');
  if (submit.disabled) return;
  submit.disabled = true;
  dom["upload-status"].textContent = "";
  try {
    const response = await fetch("/api/memes", { method: "POST", body: new FormData(dom["add-form"]) });
    if (!response.ok) throw new Error(await response.text());
    dom["add-modal"].close();
    await loadMemes();
  } catch (error) {
    dom["upload-status"].textContent = `could not upload meme: ${error.message}`;
  } finally {
    submit.disabled = false;
  }
});

dom["copy-btn"].addEventListener("click", async () => {
  const url = dom["preview-img"].src;
  dom["copy-status"].textContent = "copying…";
  try {
    if (!window.isSecureContext || !navigator.clipboard?.write || !window.ClipboardItem) {
      throw new Error("clipboard needs HTTPS and a browser with image clipboard support");
    }
    const type = { gif: "image/gif", png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", webp: "image/webp" }[url.split(".").pop().toLowerCase()];
    if (!type || (ClipboardItem.supports && !ClipboardItem.supports(type))) {
      throw new Error("this browser cannot copy the original image format");
    }
    // Start the clipboard write during the click. Keep the original format and GIF frames.
    const blob = fetch(url).then(async (response) => {
      if (!response.ok) throw new Error("could not read the original image");
      return response.blob();
    });
    await navigator.clipboard.write([new ClipboardItem({ [type]: blob })]);
    dom["copy-status"].textContent = "copied! The receiving app controls format support.";
  } catch (error) {
    dom["copy-status"].textContent = `${error.message}. Use download original instead.`;
  }
});

dom["search-input"].addEventListener("input", renderGallery);
dom["sort-input"].addEventListener("change", renderGallery);
dom["tag-filters"].addEventListener("change", renderGallery);
dom["retry-btn"].addEventListener("click", loadMemes);
window.addEventListener("keydown", (event) => {
  if (event.ctrlKey || event.metaKey || event.altKey || event.target.closest("input, textarea, select, [contenteditable]")) return;
  if (dom["add-modal"].open || dom["preview-modal"].open) return;
  if (event.key === "a") { event.preventDefault(); openAddModal(); }
  if (event.key === "/") { event.preventDefault(); dom["search-input"].focus(); }
});
window.addEventListener("paste", (event) => {
  const file = Array.from(event.clipboardData.files).find((file) => file.type.startsWith("image/"));
  if (file) { event.preventDefault(); openAddModal(file); }
});
window.addEventListener("dragover", (event) => {
  if (!Array.from(event.dataTransfer.types).includes("Files")) return;
  event.preventDefault();
  document.body.classList.add("drag-over");
});
window.addEventListener("dragleave", (event) => {
  if (!event.relatedTarget) document.body.classList.remove("drag-over");
});
window.addEventListener("drop", (event) => {
  event.preventDefault();
  document.body.classList.remove("drag-over");
  const file = Array.from(event.dataTransfer.files).find((file) => file.type.startsWith("image/"));
  if (file) openAddModal(file);
});

loadMemes();
