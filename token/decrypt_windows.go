//go:build windows
// +build windows

package token

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"

	"github.com/billgraziano/dpapi"
)

// decryptWithMasterKey decrypts a Chromium-style encrypted value (as Discord
// stores its token on Windows) using the OS-encrypted master key from
// Discord's "Local State" file. The master key is base64-encoded and prefixed
// with "DPAPI"; we strip that prefix, unwrap the key via DPAPI, then AES-GCM
// decrypt the buffer (a 3-byte version tag, a 12-byte nonce, then ciphertext).
func decryptWithMasterKey(buff []byte, masterKey string) (string, error) {
	decodedKey, err := base64.StdEncoding.DecodeString(masterKey)
	if err != nil {
		return "", err
	}

	unprotectedMasterKey, err := dpapi.DecryptBytes(decodedKey[5:])
	if err != nil {
		return "", fmt.Errorf("DPAPI decryption failed: %w", err)
	}

	nonce := buff[3:15]
	encryptedData := buff[15:]

	block, err := aes.NewCipher(unprotectedMasterKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	decryptedData, err := aesGCM.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", err
	}

	return string(decryptedData), nil
}
