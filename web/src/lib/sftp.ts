/** The host part of a URL: IPv6 addresses go in brackets. */
export function urlHost(host: string): string {
  return host.includes(":") && !host.startsWith("[") ? `[${host}]` : host;
}

/**
 * An sftp:// link that WinSCP, FileZilla, Cyberduck and most other clients
 * open. Usernames hold an email address, so the @ has to be escaped. A
 * password, when given, signs the client in without asking.
 */
export function sftpLink(host: string, port: number, user: string, password?: string): string {
  const auth = encodeURIComponent(user) + (password ? `:${encodeURIComponent(password)}` : "");
  return `sftp://${auth}@${urlHost(host)}${port && port !== 22 ? `:${port}` : ""}/`;
}

/** The host SFTP clients should use: the node's, or this panel's when the node has none set. */
export function sftpHost(nodeHost: string, location: Pick<Location, "hostname"> = window.location): { host: string; guessed: boolean } {
  if (nodeHost) return { host: nodeHost, guessed: false };
  return { host: location.hostname.replace(/^\[|\]$/g, ""), guessed: true };
}
