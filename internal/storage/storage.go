package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Storage is the object-storage abstraction shared by local disk and S3
// backends. Put/Get/Delete operate on slash-separated keys relative to the
// backend root (bucket for S3, data dir for local). SaveDocument and
// RemoveDocument are compat helpers used by the documents service.
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	SaveDocument(ctx context.Context, id string, r io.Reader) (string, error)
	RemoveDocument(ctx context.Context, id string) error
}

var (
	_ Storage = (*LocalStore)(nil)
	_ Storage = (*S3Storage)(nil)
)

// LocalStorage is an alias for LocalStore so callers can use either name.
type LocalStorage = LocalStore

// NewLocalStorage is an alias for NewLocalStore.
func NewLocalStorage(root string) (*LocalStore, error) {
	return NewLocalStore(root)
}

// validKey reports whether key is a safe slash-separated object key.
func validKey(key string) bool {
	if key == "" || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Put stores r at key relative to the local root using temp-file + rename.
func (s *LocalStore) Put(_ context.Context, key string, r io.Reader) error {
	if !validKey(key) {
		return fmt.Errorf("storage: unsafe key %q", key)
	}
	dest := filepath.Join(s.root, filepath.FromSlash(key))
	if !withinRoot(s.root, dest) {
		return fmt.Errorf("storage: resolved path escapes root: %q", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("storage: create directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "put-*")
	if err != nil {
		return fmt.Errorf("storage: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("storage: write file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("storage: close file: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("storage: finalize file: %w", err)
	}
	return nil
}

// Get opens the object at key for reading.
func (s *LocalStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, fmt.Errorf("storage: unsafe key %q", key)
	}
	p := filepath.Join(s.root, filepath.FromSlash(key))
	if !withinRoot(s.root, p) {
		return nil, fmt.Errorf("storage: resolved path escapes root: %q", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	return f, nil
}

// Delete removes the object at key. Missing keys are a no-op.
func (s *LocalStore) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return fmt.Errorf("storage: unsafe key %q", key)
	}
	p := filepath.Join(s.root, filepath.FromSlash(key))
	if !withinRoot(s.root, p) {
		return fmt.Errorf("storage: resolved path escapes root: %q", p)
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}

// S3Config holds S3-compatible object storage settings.
type S3Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
}

// S3ConfigFromEnv reads S3 settings from the environment:
// S3_ENDPOINT, S3_BUCKET, S3_REGION, AWS_ACCESS_KEY_ID (or AWS_ACCESS_KEY),
// AWS_SECRET_ACCESS_KEY (or AWS_SECRET_KEY).
func S3ConfigFromEnv() S3Config {
	access := os.Getenv("AWS_ACCESS_KEY_ID")
	if access == "" {
		access = os.Getenv("AWS_ACCESS_KEY")
	}
	secret := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if secret == "" {
		secret = os.Getenv("AWS_SECRET_KEY")
	}
	region := os.Getenv("S3_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	return S3Config{
		Endpoint:  strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		Bucket:    strings.TrimSpace(os.Getenv("S3_BUCKET")),
		Region:    strings.TrimSpace(region),
		AccessKey: strings.TrimSpace(access),
		SecretKey: strings.TrimSpace(secret),
	}
}

// S3Storage is an S3-compatible backend using only stdlib net/http with
// AWS Signature Version 4.
type S3Storage struct {
	endpoint string
	bucket   string
	region   string
	access   string
	secret   string
	client   *http.Client
	now      func() time.Time
}

// NewS3Storage validates cfg and returns a backend. An empty endpoint
// defaults to AWS S3 for the configured region.
func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("storage: S3_BUCKET is required")
	}
	if strings.Contains(bucket, "/") {
		return nil, fmt.Errorf("storage: invalid S3_BUCKET %q", cfg.Bucket)
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	endpoint := strings.TrimSuffix(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("storage: AWS access key and secret key are required")
	}
	return &S3Storage{
		endpoint: endpoint,
		bucket:   bucket,
		region:   region,
		access:   strings.TrimSpace(cfg.AccessKey),
		secret:   strings.TrimSpace(cfg.SecretKey),
		client:   &http.Client{Timeout: 30 * time.Second},
		now:      time.Now,
	}, nil
}

// NewS3StorageFromEnv builds an S3Storage from environment variables.
func NewS3StorageFromEnv() (*S3Storage, error) {
	return NewS3Storage(S3ConfigFromEnv())
}

// NewStorageFromConfig returns an S3Storage when backend is "s3" (case
// insensitive), otherwise a LocalStore rooted at dataDir. s3cfg supplies the
// S3 settings (typically built from AppConfig); validation errors from the
// selected backend are returned so callers fail fast at startup.
func NewStorageFromConfig(backend, dataDir string, s3cfg S3Config) (Storage, error) {
	if strings.EqualFold(strings.TrimSpace(backend), "s3") {
		return NewS3Storage(s3cfg)
	}
	return NewLocalStore(dataDir)
}

// NewStorageFromEnv returns an S3Storage when STORAGE_BACKEND=s3 (case
// insensitive), otherwise a LocalStore rooted at dataDir.
func NewStorageFromEnv(dataDir string) (Storage, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("STORAGE_BACKEND")), "s3") {
		return NewS3StorageFromEnv()
	}
	return NewLocalStore(dataDir)
}

func (s *S3Storage) objectURL(key string) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("storage: unsafe key %q", key)
	}
	segs := strings.Split(key, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return s.endpoint + "/" + s.bucket + "/" + strings.Join(segs, "/"), nil
}

func hashPayload(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var emptySHA256 = hashPayload(nil)

func (s *S3Storage) sign(req *http.Request, payloadHash string, t time.Time) {
	amzDate := t.UTC().Format("20060102T150405Z")
	dateStamp := t.UTC().Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	host := req.URL.Host
	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQS := ""
	if req.URL.RawQuery != "" {
		q, _ := url.ParseQuery(req.URL.RawQuery)
		canonicalQS = q.Encode()
	}

	canonicalRequest := req.Method + "\n" +
		canonicalURI + "\n" +
		canonicalQS + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash

	h := sha256.Sum256([]byte(canonicalRequest))
	scope := dateStamp + "/" + s.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(h[:])

	mac := func(key []byte, data string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(data))
		return m.Sum(nil)
	}
	kDate := mac([]byte("AWS4"+s.secret), dateStamp)
	kRegion := mac(kDate, s.region)
	kService := mac(kRegion, "s3")
	kSigning := mac(kService, "aws4_request")
	sig := mac(kSigning, stringToSign)

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.access+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+hex.EncodeToString(sig))
}

func (s *S3Storage) do(req *http.Request, payloadHash string) (*http.Response, error) {
	s.sign(req, payloadHash, s.now())
	client := s.client
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// Put uploads r as the object at key.
func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader) error {
	u, err := s.objectURL(key)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("storage: read upload body: %w", err)
	}
	ph := hashPayload(data)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("storage: build request: %w", err)
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := s.do(req, ph)
	if err != nil {
		return fmt.Errorf("storage: s3 put: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("storage: s3 put %q: status %s", key, resp.Status)
	}
	return nil
}

// Get downloads the object at key.
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: build request: %w", err)
	}
	resp, err := s.do(req, emptySHA256)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("storage: s3 get %q: status %s", key, resp.Status)
	}
	return resp.Body, nil
}

// Delete removes the object at key. A 404 is treated as success.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	u, err := s.objectURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("storage: build request: %w", err)
	}
	resp, err := s.do(req, emptySHA256)
	if err != nil {
		return fmt.Errorf("storage: s3 delete: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("storage: s3 delete %q: status %s", key, resp.Status)
	}
	return nil
}

// documentKey returns the canonical object key for a document id.
func documentKey(id string) (string, error) {
	if !safeSegment(id) {
		return "", fmt.Errorf("storage: unsafe document id %q", id)
	}
	return documentLayout + "/" + id + "/original.pdf", nil
}

// SaveDocument uploads content as the document's canonical key and returns
// the slash-separated key.
func (s *S3Storage) SaveDocument(ctx context.Context, id string, r io.Reader) (string, error) {
	key, err := documentKey(id)
	if err != nil {
		return "", err
	}
	if err := s.Put(ctx, key, r); err != nil {
		return "", err
	}
	return key, nil
}

// RemoveDocument deletes the document's canonical object.
func (s *S3Storage) RemoveDocument(ctx context.Context, id string) error {
	key, err := documentKey(id)
	if err != nil {
		return err
	}
	return s.Delete(ctx, key)
}
