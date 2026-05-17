// copyText writes text to the clipboard with a textarea fallback for
// non-secure contexts (http://<lan-ip>, non-localhost). The Clipboard API
// is only available on HTTPS or localhost; without this fallback the copy
// button silently fails when the dashboard is served over plain HTTP.
export async function copyText(text: string): Promise<boolean> {
  // Preferred path: secure context with Clipboard API.
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to legacy path
    }
  }

  // Legacy fallback: invisible textarea + document.execCommand('copy').
  // Works on http:// origins where Clipboard API is blocked.
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.top = '0';
  ta.style.left = '0';
  ta.style.opacity = '0';
  ta.style.pointerEvents = 'none';
  document.body.appendChild(ta);
  ta.focus();
  ta.select();
  ta.setSelectionRange(0, text.length);

  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  document.body.removeChild(ta);
  return ok;
}
