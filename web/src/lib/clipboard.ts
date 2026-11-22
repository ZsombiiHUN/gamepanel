import i18n from "../i18n";
/**
 * Copies text to the clipboard. Rejects where the Clipboard API is missing: browsers only
 * offer it on HTTPS and localhost, so a panel reached over plain HTTP has none.
 */
export async function copyText(text: string): Promise<void> {
  // The DOM types say the API is always there; plain-HTTP origins disagree.
  if (!("clipboard" in navigator)) throw new Error(i18n.t("clipboard.unavailable"));
  await navigator.clipboard.writeText(text);
}
