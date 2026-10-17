document.querySelectorAll("[data-copy]").forEach(button => {
  button.addEventListener("click", async () => {
    const text = document.getElementById(button.dataset.copy).textContent;
    const status = document.getElementById("copy-status");
    try {
      await navigator.clipboard.writeText(text);
      button.textContent = "Copied";
      status.textContent = "Install command copied to clipboard.";
      setTimeout(() => { button.textContent = "Copy"; }, 1800);
    } catch {
      const range = document.createRange();
      range.selectNodeContents(document.getElementById(button.dataset.copy));
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      status.textContent = "Command selected. Copy it with your keyboard.";
    }
  });
});

fetch("./releases.json").then(response => {
  if (!response.ok) throw new Error("Release information unavailable");
  return response.json();
}).then(release => {
  if (!/^\d+\.\d+\.\d+(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?$/.test(release.version)) return;
  const status = document.getElementById("release-status");
  status.replaceChildren(document.createTextNode(`Current preview: ${release.version} · `));
  const checksums = document.createElement("a");
  checksums.href = release.checksums_url;
  checksums.textContent = "Verify download checksums";
  status.append(checksums);
  document.getElementById("release-link").href = release.html_url;
}).catch(() => { /* The static GitHub Releases link remains usable. */ });
