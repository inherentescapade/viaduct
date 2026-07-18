package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// linkDiscordID records that cs's account belongs to the given verified
// Discord user ID, and reconciles it with whatever else the server already
// knows about that user. discordID must come from a value Discord itself
// just confirmed (e.g. newResolver/ValidateToken) — never from unverified,
// client-supplied data. (A token's leading base64 segment does decode to a
// user ID without any network call, but trusting that for routing WITHOUT
// verification would let anyone who merely knows — not holds — a target's
// Discord ID craft a garbage token that decodes to it and hijack routing
// into that account before any credential is ever checked. Verification is
// what makes this safe.)
//
//   - If another account is already canonical for this user, cs was reached
//     under a different token string for the SAME person (tokens aren't
//     stable across devices or re-logins) — cs merges into it.
//   - If this is the first time this process has confirmed this user, any
//     OTHER account nobody has yet confirmed an identity for is checked too,
//     by re-validating ITS OWN stored (already server-trusted) token against
//     Discord — this is how an account whose one and only client key later
//     rotated to a new token, with no key ever revisiting the old one, gets
//     reunited with its history instead of staying orphaned forever.
func (s *Server) linkDiscordID(cs *clientState, discordID string) {
	if discordID == "" {
		return
	}

	s.clientsMu.Lock()
	canonicalKey, seen := s.byDiscordID[discordID]
	if seen {
		s.clientsMu.Unlock()
		if canonicalKey == cs.key {
			return
		}
		canonical, ok := s.lookupAccount(canonicalKey)
		if !ok {
			// The canonical account vanished (shouldn't happen); adopt cs instead
			// of leaving byDiscordID pointing at nothing.
			s.registerDiscordID(discordID, cs.key)
			return
		}
		s.mergeAccounts(cs, canonical)
		return
	}

	// First sighting of this user this process. Gather every other account
	// nobody has yet confirmed an identity for, so an orphan sharing this same
	// person can be reunited with them below.
	orphans := s.unlinkedAccountsLocked(cs.key)
	s.byDiscordID[discordID] = cs.key
	s.clientsMu.Unlock()

	for _, other := range orphans {
		creds := other.currentCreds()
		if creds.Token == "" {
			continue
		}
		_, res, err := s.buildEngine(creds)
		if err != nil {
			continue // this account's own stored token no longer validates; leave it
		}
		if res.user.Id == discordID {
			s.mergeAccounts(other, cs)
			continue
		}
		s.registerDiscordID(res.user.Id, other.key)
	}
}

func (s *Server) lookupAccount(key string) (*clientState, bool) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	cs, ok := s.clients[key]
	return cs, ok
}

// registerDiscordID records discordID -> key if discordID isn't already
// registered (first writer wins; if two are somehow discovered concurrently,
// the next time either is seen again resolves it).
func (s *Server) registerDiscordID(discordID, key string) {
	s.clientsMu.Lock()
	if _, ok := s.byDiscordID[discordID]; !ok {
		s.byDiscordID[discordID] = key
	}
	s.clientsMu.Unlock()
}

// unlinkedAccountsLocked returns every account other than exceptKey that
// isn't yet registered as someone's canonical account. Caller holds
// clientsMu.
func (s *Server) unlinkedAccountsLocked(exceptKey string) []*clientState {
	linked := make(map[string]bool, len(s.byDiscordID))
	for _, key := range s.byDiscordID {
		linked[key] = true
	}
	var out []*clientState
	for key, cs := range s.clients {
		if key == exceptKey || linked[key] {
			continue
		}
		out = append(out, cs)
	}
	return out
}

// mergeAccounts folds dup into canonical: dup's monitors and deletion logs
// move onto canonical (dup's monitors are then deleted so its still-running
// scheduler loop can't duplicate-execute them — there's no per-account
// cancellation, so the loop itself just keeps ticking harmlessly forever),
// any client key currently routed to dup gets repointed to canonical, and
// dup's own persisted account entry is cleared.
func (s *Server) mergeAccounts(dup, canonical *clientState) {
	if dup.key == canonical.key {
		return
	}

	s.clientsMu.Lock()
	delete(s.clients, dup.key)
	var movedClientKeys []string
	for clientKey, acct := range s.keyToAccount {
		if acct == dup.key {
			s.keyToAccount[clientKey] = canonical.key
			movedClientKeys = append(movedClientKeys, clientKey)
		}
	}
	s.clientsMu.Unlock()

	for _, p := range dup.monitors.List() {
		id := p.ID
		p.ID = ""
		if _, err := canonical.monitors.Upsert(p); err != nil {
			s.logf("account merge: could not move monitor %q: %v", p.Name, err)
			continue
		}
		dup.monitors.Delete(id)
	}
	if moved, err := moveLogFiles(dup.logDir, canonical.logDir); err != nil {
		s.logf("account merge: could not move deletion logs: %v", err)
	} else if moved > 0 {
		s.logf("account merge: moved %d deletion log file(s) into the shared account", moved)
	}

	for _, clientKey := range movedClientKeys {
		s.persistClientKey(clientKey, canonical.key)
	}
	s.persistAccount(dup.key, Credentials{})
	s.logf("merged account %s into %s", dup.key, canonical.key)
}

// moveLogFiles relocates every file from src into dst, disambiguating any
// filename collision so a merge never overwrites a log. It's a no-op if src
// doesn't exist. src is deliberately left in place (even once empty) rather
// than removed — a job that's still actively writing into it (a merge can
// race a running deletion) needs somewhere to land.
func moveLogFiles(src, dst string) (int, error) {
	entries, err := os.ReadDir(src)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dst, 0700); err != nil {
		return 0, err
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := uniqueLogPath(filepath.Join(dst, e.Name()))
		if err := os.Rename(filepath.Join(src, e.Name()), target); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// uniqueLogPath returns path, or a disambiguated sibling if path already
// exists, so two accounts merging into one never collide on a log's filename.
func uniqueLogPath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
