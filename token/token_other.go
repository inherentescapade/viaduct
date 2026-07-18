//go:build !windows && !linux
// +build !windows,!linux

package token

import "errors"

func GetTokens() ([]string, error) {
	return nil, errors.New("automatic token extraction is not supported on this platform")
}
