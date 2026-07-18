// friendlyError turns a raw backend/Go error into a short, human message. The
// server client wraps net errors like "could not reach server at host: dial
// tcp ...: connection refused", which is noise to a user, so map the common cases.
export function friendlyError(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e);
  const m = raw.toLowerCase();

  if (
    m.includes("could not reach") ||
    m.includes("connection refused") ||
    m.includes("dial tcp") ||
    m.includes("no such host") ||
    m.includes("timeout") ||
    m.includes("i/o timeout") ||
    m.includes("eof") ||
    m.includes("connection reset")
  ) {
    return "Can't reach the server. Check it's running and the address is correct.";
  }
  if (m.includes("decrypt") || m.includes("impostor") || m.includes("server key")) {
    return "Couldn't verify the server. The server key may be wrong.";
  }
  if (m.includes("unauthorized")) {
    return "This client isn't authorized on the server.";
  }
  if (m.includes("no discord token") || m.includes("has no discord token")) {
    return "The server has no Discord token yet. Send one from the Server tab.";
  }
  // Otherwise the message is already a deliberate, user-facing one from the
  // server (e.g. "no DM conversations found"), so show it as-is.
  return raw;
}
