// Package accounts keeps the cookies of sites the user signed in to, so
// downloads that need a login can pass them to yt-dlp.
//
// Snapshots are encrypted with AES-256-GCM under a random key stored next
// to them. That does not stop a program running as the same user — it can
// read the key as well — but it keeps the cookies out of backups, synced
// folders and data folders sent along with a bug report, and away from
// stealers that look for cookies.txt files.
package accounts

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const (
	keyFileName = "key"
	keySize     = 32
	// snapshotMagic starts every snapshot file and versions its format:
	// v2 binds the snapshot to its user and site, v1 (written before there
	// were users) to the site only.
	snapshotMagic   = "YTGC2"
	snapshotMagicV1 = "YTGC1"
	// LegacyUser owns the v1 snapshots: the desktop's only user.
	LegacyUser = "local"
)

var errCorruptSnapshot = errors.New("снимок cookies повреждён или зашифрован другим ключом")

// Store reads and writes encrypted cookie snapshots in one directory.
type Store struct {
	dir string

	keyMu sync.Mutex
	key   []byte

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, locks: map[string]*sync.Mutex{}}
}

func (s *Store) Dir() string {
	return s.dir
}

// Lock serialises changes to one account's snapshot (an import racing a
// download's write-back); call the returned function to unlock.
func (s *Store) Lock(id string) func() {
	s.locksMu.Lock()
	mu, ok := s.locks[id]
	if !ok {
		mu = &sync.Mutex{}
		s.locks[id] = mu
	}
	s.locksMu.Unlock()

	mu.Lock()
	return mu.Unlock
}

// UseKey makes the store encrypt with key instead of the key file next
// to the snapshots: a server keeps its key outside the data volume
// (YTG_SECRET_KEY_FILE), so a copy of the volume doesn't carry it.
func (s *Store) UseKey(key []byte) error {
	if len(key) != keySize {
		return fmt.Errorf("ключ должен быть %d байт", keySize)
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	s.key = key
	return nil
}

// DeriveKey turns the contents of a secret file (e.g. the output of
// "openssl rand -base64 32") into a key for UseKey.
func DeriveKey(secret []byte) ([]byte, error) {
	secret = bytes.TrimSpace(secret)
	if len(secret) < keySize {
		return nil, fmt.Errorf("секрет короче %d байт: создайте его, например, командой openssl rand -base64 32", keySize)
	}
	sum := sha256.Sum256(append([]byte("ytgrabber cookies key\x00"), secret...))
	return sum[:], nil
}

// additionalData binds a snapshot to its user and site, so a snapshot
// copied over another account's file, the same user's or not, fails to
// decrypt instead of being used.
func additionalData(user string, site string) []byte {
	return []byte(user + "\x00" + site)
}

// Save encrypts cookies into the account's snapshot, replacing it
// atomically.
func (s *Store) Save(id string, user string, site string, cookies []Cookie) error {
	key, err := s.loadKey(true)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	data := append([]byte(snapshotMagic), nonce...)
	data = gcm.Seal(data, nonce, FormatCookies(cookies), additionalData(user, site))
	return WritePrivateFile(s.snapshotPath(id), data)
}

// Load decrypts the account's snapshot.
func (s *Store) Load(id string, user string, site string) ([]Cookie, error) {
	data, err := os.ReadFile(s.snapshotPath(id))
	if err != nil {
		return nil, err
	}
	key, err := s.loadKey(false)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	aad := additionalData(user, site)
	switch {
	case bytes.HasPrefix(data, []byte(snapshotMagic)):
	case bytes.HasPrefix(data, []byte(snapshotMagicV1)) && user == LegacyUser:
		aad = []byte(site) // rewritten as v2 on the next Save
	default:
		return nil, errCorruptSnapshot
	}
	data = data[len(snapshotMagic):]
	if len(data) < gcm.NonceSize() {
		return nil, errCorruptSnapshot
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], aad)
	if err != nil {
		return nil, errCorruptSnapshot
	}
	return ParseCookies(bytes.NewReader(plain))
}

// Delete removes the account's snapshot; a missing one is not an error.
func (s *Store) Delete(id string) error {
	if err := os.Remove(s.snapshotPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) snapshotPath(id string) string {
	return filepath.Join(s.dir, filepath.Base(id)+".enc")
}

// loadKey reads the key file, creating it first when create is set (on
// the first saved account).
func (s *Store) loadKey(create bool) ([]byte, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.key != nil {
		return s.key, nil
	}

	path := filepath.Join(s.dir, keyFileName)
	key, err := os.ReadFile(path)
	switch {
	case err == nil && len(key) == keySize:
	case err == nil:
		return nil, fmt.Errorf("файл ключа %s повреждён", path)
	case errors.Is(err, fs.ErrNotExist) && create:
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := WritePrivateFile(path, key); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("не удалось прочитать ключ: %w", err)
	}
	s.key = key
	return key, nil
}

// EnsurePrivateDir creates dir readable only by the current user (0700;
// on Windows the per-user profile folder already restricts access).
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// WritePrivateFile writes data to path with 0600 permissions, via a
// temporary file and a rename so readers never see half a file.
func WritePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := EnsurePrivateDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
