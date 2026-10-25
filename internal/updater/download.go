package updater

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// PublicKey verifies the signature of a release's SHA256SUMS.txt (base64
// Ed25519 public key). Release builds set it from the repository variable
// UPDATE_PUBLIC_KEY (see .github/workflows/release.yml):
//
//	-ldflags "-X ytgrabber/internal/updater.PublicKey=..."
//
// Without it the app only offers the release page: installing an update
// that nothing but its own checksum file vouches for would let whoever
// can replace release files replace the app.
var PublicKey = ""

const maxChecksumsSize = 1 << 20

var errNoSignature = errors.New("релиз не подписан")

// VerifiedChecksums downloads the release's checksum list and its
// signature, verifies it against key and returns the SHA-256 of every
// file in it.
func (c *Client) VerifiedChecksums(ctx context.Context, release *Release, key ed25519.PublicKey) (map[string]string, error) {
	sumsAsset, ok := release.Asset(checksumsAsset)
	if !ok {
		return nil, errNoSignature
	}
	sigAsset, ok := release.Asset(signatureAsset)
	if !ok {
		return nil, errNoSignature
	}

	sums, err := c.fetchSmall(ctx, sumsAsset.URL)
	if err != nil {
		return nil, err
	}
	sigText, err := c.fetchSmall(ctx, sigAsset.URL)
	if err != nil {
		return nil, err
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, sums, signature) {
		return nil, errors.New("подпись списка контрольных сумм не сходится: обновление не установлено")
	}
	return ParseChecksums(sums), nil
}

// ParseChecksums reads `sha256sum` output: "<hex>  <name>" per line.
func ParseChecksums(data []byte) map[string]string {
	sums := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	return sums
}

// ParsePublicKey decodes PublicKey; ok is false when it is unset or
// malformed.
func ParsePublicKey(encoded string) (ed25519.PublicKey, bool) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(key), true
}

// Download saves asset to path, checking it against wantSHA256 on the way.
// progress gets the bytes received so far and the expected total.
func (c *Client) Download(ctx context.Context, asset Asset, wantSHA256 string, path string, progress func(done, total int64)) error {
	resp, err := c.get(ctx, asset.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	total := resp.ContentLength
	if total <= 0 {
		total = asset.Size
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	hash := sha256.New()
	counter := &progressWriter{total: total, report: progress}
	_, copyErr := io.Copy(io.MultiWriter(file, hash, counter), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("загрузка прервалась: %w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != strings.ToLower(wantSHA256) {
		os.Remove(path)
		return fmt.Errorf("контрольная сумма %s не совпадает: файл повреждён или подменён", asset.Name)
	}
	return nil
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось скачать обновление: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("не удалось скачать обновление: %s", resp.Status)
	}
	return resp, nil
}

func (c *Client) fetchSmall(ctx context.Context, url string) ([]byte, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxChecksumsSize {
		return nil, errors.New("файл контрольных сумм слишком большой")
	}
	return data, nil
}

type progressWriter struct {
	done, total int64
	report      func(done, total int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if w.report != nil {
		w.report(w.done, w.total)
	}
	return len(p), nil
}
