// Secure bundles (.ksx) are the opaque answer to plain .kslib files.
//
// A plain .kslib is JSON with full .ks sources: `cat` reveals everything
// and the `kslib-1` tag reveals the language. A .ksx file is instead:
//
//	plaintext  = gzip(JSON(Bundle{format:"kslib-1", ...}))
//	file bytes = nonce(12) + AES-256-GCM(plaintext)
//
// There is no magic header, no shebang, no JSON keys in the clear: the
// file looks like random bytes, so `cat`, `strings` and `file` reveal
// nothing about the source or the language.
//
// Key model:
//   - `fusion build --secure` (no password): obfuscation mode. The key is
//     derived from a built-in default secret. It stops casual reading but
//     anyone reading this open-source repo can recover it. Good for hiding
//     code from customers, NOT for real secrets.
//   - `fusion build --secure --password X` (or --key-file / FUSION_KEY):
//     real safety. Key = SHA256(password). Without the password the bundle
//     cannot be decrypted (AES-256-GCM auth fails). The password is never
//     stored in the bundle.
//
// At load time the bundle is decrypted + decompressed strictly in memory
// and executed; nothing decrypted is ever written to disk.
package lib

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/config"
)

// SecureExt is the opaque bundle file extension.
const SecureExt = ".ksx"

// defaultSecret backs `--secure` without a password. Obfuscation only:
// the source is open, so treat this as hiding from `cat`, not from a
// determined reverser. Pass --password for real safety.
const defaultSecret = "ks-fusion-secure-default-v1::obfuscation-only::use---password-for-real-safety"

// ResolvePassword picks the effective password: explicit flag wins, then
// FUSION_KEY env, then "" (default-secret mode).
func ResolvePassword(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("FUSION_KEY"); v != "" {
		return v
	}
	return ""
}

// KeyFromPassword derives the 32-byte AES key. Empty password means the
// built-in default secret (opaque but recoverable); any other password
// gives real encryption.
func KeyFromPassword(pw string) []byte {
	pw = ResolvePassword(pw)
	if pw == "" {
		pw = defaultSecret
	}
	sum := sha256.Sum256([]byte(pw))
	return sum[:]
}

// ReadKeyFile reads a password from a file (trailing newline trimmed).
func ReadKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ArtifactNameSecure returns "<name>-<version>.ksx".
func ArtifactNameSecure(name, version string) string {
	return fmt.Sprintf("%s-%s%s", name, version, SecureExt)
}

// IsSecurePath reports whether path looks like a secure bundle.
func IsSecurePath(path string) bool {
	return strings.HasSuffix(path, SecureExt)
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func encryptGCM(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

func decryptGCM(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("bad secure bundle (bad nonce)")
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// BuildSecure parse-checks the lib package and writes an opaque .ksx
// bundle into outDir, returning the bundle file path. password "" means
// default-secret (opaque) mode; any other value means real encryption.
func BuildSecure(cfg *config.Config, outDir, password string) (string, error) {
	if !cfg.IsLib() {
		return "", fmt.Errorf("%s is not a library (set type = \"lib\" in fusion.toml)", cfg.Dir)
	}
	files, err := Check(cfg)
	if err != nil {
		return "", err
	}
	b := &Bundle{Format: Format, Name: cfg.LibName, Version: cfg.Version, Files: files}
	plain, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	comp, err := gzipBytes(plain)
	if err != nil {
		return "", err
	}
	key := KeyFromPassword(password)
	nonce, ct, err := encryptGCM(key, comp)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(outDir, ArtifactNameSecure(b.Name, b.Version))
	blob := append(append([]byte{}, nonce...), ct...)
	if err := os.WriteFile(out, blob, 0o644); err != nil {
		return "", err
	}
	return out, nil
}

// LoadSecure reads and decrypts a .ksx bundle file. password "" uses
// FUSION_KEY env or the default secret (see ResolvePassword).
func LoadSecure(path, password string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// GCM nonce is 12 bytes; anything shorter cannot be a .ksx.
	if len(data) < 12+16 {
		return nil, fmt.Errorf("bad secure bundle %s: too short (wrong key or not a .ksx?)", path)
	}
	key := KeyFromPassword(password)
	nonce, ct := data[:12], data[12:]
	comp, err := decryptGCM(key, nonce, ct)
	if err != nil {
		return nil, fmt.Errorf("bad secure bundle %s: decrypt failed (wrong key? set FUSION_KEY or --password): %v", path, err)
	}
	plain, err := gunzipBytes(comp)
	if err != nil {
		// Back-compat: allow uncompressed payloads.
		plain = comp
	}
	var b Bundle
	if err := json.Unmarshal(plain, &b); err != nil {
		return nil, fmt.Errorf("bad secure bundle %s: corrupt payload: %w", path, err)
	}
	if b.Format != Format {
		return nil, fmt.Errorf("bad secure bundle %s: unknown format %q", path, b.Format)
	}
	if b.Name == "" || len(b.Files) == 0 {
		return nil, fmt.Errorf("bad secure bundle %s: empty name or sources", path)
	}
	return &b, nil
}

// LoadAny loads either a plain .kslib or a secure .ksx bundle.
// .ksx paths always use secure loading; other paths try plain first
// then secure (so renamed .ksx files e.g. lib.bin still work).
func LoadAny(path, password string) (*Bundle, error) {
	if IsSecurePath(path) {
		return LoadSecure(path, password)
	}
	if b, err := Load(path); err == nil {
		return b, nil
	}
	// Fall back to secure (renamed .ksx without extension).
	if b, err := LoadSecure(path, password); err == nil {
		return b, nil
	}
	// Return the plain error (more familiar for .kslib users).
	return Load(path)
}
