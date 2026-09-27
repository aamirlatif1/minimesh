// Package wg wraps WireGuard key handling (and, from M4, device configuration).
package wg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// LoadOrCreateKey reads a base64 private key from path, or generates one and
// writes it (mode 0600) if the file does not exist. The public key is the
// peer's identity everywhere: coordinator, signalling, relay and WireGuard.
func LoadOrCreateKey(path string) (wgtypes.Key, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		k, err := wgtypes.ParseKey(strings.TrimSpace(string(b)))
		if err != nil {
			return wgtypes.Key{}, fmt.Errorf("parse %s: %w", path, err)
		}
		return k, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return wgtypes.Key{}, err
	}

	k, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return wgtypes.Key{}, err
	}
	if err := os.WriteFile(path, []byte(k.String()+"\n"), 0o600); err != nil {
		return wgtypes.Key{}, err
	}
	return k, nil
}
