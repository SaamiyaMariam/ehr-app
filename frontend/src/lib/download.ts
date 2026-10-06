import { apiFetch } from "@/lib/api";

// Opens an authenticated PDF in a new tab. The tab is opened synchronously
// (inside the click) so pop-up blockers allow it, then pointed at the file.
export async function openPdf(path: string) {
  const tab = window.open("", "_blank");

  try {
    const response = await apiFetch(path);

    if (!response.ok) {
      let message = `Unable to open the PDF (${response.status})`;
      try {
        message = (await response.json()).error || message;
      } catch {
        // not JSON
      }
      throw new Error(message);
    }

    const blob = await response.blob();
    const url = URL.createObjectURL(new Blob([blob], { type: "application/pdf" }));

    if (tab) {
      tab.location.href = url;
    } else {
      window.location.href = url;
    }

    setTimeout(() => URL.revokeObjectURL(url), 60_000);
  } catch (error) {
    tab?.close();
    throw error;
  }
}

// Downloads an authenticated API file (PDF / CSV) via a temporary object URL.
export async function downloadFile(path: string, fallbackName: string) {
  const response = await apiFetch(path);

  if (!response.ok) {
    let message = `Download failed (${response.status})`;
    try {
      const data = await response.json();
      message = data.error || message;
    } catch {
      // not JSON
    }
    throw new Error(message);
  }

  const disposition = response.headers.get("Content-Disposition") ?? "";
  const match = disposition.match(/filename="([^"]+)"/);
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);

  const link = document.createElement("a");
  link.href = url;
  link.download = match?.[1] ?? fallbackName;
  document.body.appendChild(link);
  link.click();
  link.remove();

  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
