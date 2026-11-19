/**
 * Copies text to the clipboard. Rejects where the Clipboard API is missing: browsers only
 * offer it on HTTPS and localhost, so a panel reached over plain HTTP has none.
 */
export async function copyText(text: string): Promise<void> {
  // The DOM types say the API is always there; plain-HTTP origins disagree.
  if (!("clipboard" in navigator)) throw new Error("The clipboard is not available on this page.");
  await navigator.clipboard.writeText(text);
}
