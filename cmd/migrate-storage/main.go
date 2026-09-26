// Command migrate-storage copies local document files (./data/documents)
// to S3-compatible object storage through the shared Storage interface.
//
// Usage:
//
//	go run ./cmd/migrate-storage [--dry-run] [--verify] [--source ./data] [--prefix ""]
//
// Env:
//	STORAGE_BACKEND is ignored by this tool (destination is always S3).
//	S3_ENDPOINT, S3_BUCKET (required), S3_REGION / AWS_REGION,
//	AWS_ACCESS_KEY_ID (or AWS_ACCESS_KEY / S3_ACCESS_KEY),
//	AWS_SECRET_ACCESS_KEY (or AWS_SECRET_KEY / S3_SECRET_KEY).
//
// Flags:
//	-dry-run   list what would be uploaded without uploading.
//	-verify    after upload, Get each key back and compare SHA-256.
//	-source    local data root containing documents/ (default STORAGE_PATH or ./data).
//	-prefix    optional key prefix prepended to each relative path.
//
// Keys are the slash-separated paths relative to --source, so a local file
// <source>/documents/<id>/original.pdf> becomes the same key in the bucket,
// matching the Storage interface layout used by LocalStorage/S3Storage.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/storage"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "list files that would be uploaded without uploading")
	verify := flag.Bool("verify", false, "download each uploaded key and compare SHA-256")
	source := flag.String("source", envOr("STORAGE_PATH", "data"), "local data root containing documents/")
	prefix := flag.String("prefix", "", "optional key prefix prepended to each relative path")
	flag.Parse()

	if strings.TrimSpace(*source) == "" {
		fatal("source must not be empty")
	}
	docsDir := filepath.Join(*source, "documents")
	if st, err := os.Stat(docsDir); err != nil || !st.IsDir() {
		fatal(fmt.Sprintf("source documents dir not found: %s (run from repo root or pass --source)", docsDir))
	}

	var files []string
	if err := filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		fatal(fmt.Sprintf("walk source dir: %v", err))
	}
	if len(files) == 0 {
		fmt.Println("migrate-storage: nothing to migrate (no files under " + docsDir + ")")
		return
	}

	cfg := s3ConfigFromEnv()
	var dest storage.Storage
	if !*dryRun {
		if cfg.Bucket == "" {
			fatal("S3_BUCKET is required for the migration destination")
		}
		s3dest, err := storage.NewS3Storage(storage.S3Config{
			Endpoint:  cfg.Endpoint,
			Bucket:    cfg.Bucket,
			Region:    cfg.Region,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
		})
		if err != nil {
			fatal(fmt.Sprintf("invalid S3 config: %v", err))
		}
		dest = s3dest
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var uploaded, skipped, failed int
	for _, f := range files {
		rel, err := filepath.Rel(*source, f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP %s: %v\n", f, err)
			skipped++
			continue
		}
		key := filepath.ToSlash(rel)
		if p := strings.Trim(strings.TrimSpace(*prefix), "/"); p != "" {
			key = p + "/" + key
		}
		if *dryRun {
			fmt.Printf("WOULD PUT %s -> s3://%s/%s\n", f, cfg.Bucket, key)
			skipped++
			continue
		}
		if err := putFile(ctx, dest, key, f); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s -> %s: %v\n", f, key, err)
			failed++
			continue
		}
		fmt.Printf("PUT %s -> s3://%s/%s\n", f, cfg.Bucket, key)
		uploaded++

		if *verify {
			if err := verifyFile(ctx, dest, key, f); err != nil {
				fmt.Fprintf(os.Stderr, "VERIFY-FAIL %s: %v\n", key, err)
				failed++
				continue
			}
			fmt.Printf("VERIFY OK %s\n", key)
		}
	}

	fmt.Printf("migrate-storage done: uploaded=%d skipped=%d failed=%d (dry-run=%v verify=%v)\n",
		uploaded, skipped, failed, *dryRun, *verify)
	if failed > 0 {
		os.Exit(1)
	}
}

func putFile(ctx context.Context, dest storage.Storage, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return dest.Put(ctx, key, f)
}

func verifyFile(ctx context.Context, dest storage.Storage, key, path string) error {
	want, err := sha256File(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	rc, err := dest.Get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return err
	}
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
		return fmt.Errorf("sha256 mismatch: local=%s remote=%s", want, got)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// s3ConfigFromEnv mirrors storage.S3ConfigFromEnv but also honours the
// S3_ACCESS_KEY / S3_SECRET_KEY names used by AppConfig, so the migration
// tool works with either convention.
func s3ConfigFromEnv() storage.S3Config {
	cfg := storage.S3ConfigFromEnv()
	if cfg.AccessKey == "" {
		cfg.AccessKey = strings.TrimSpace(os.Getenv("S3_ACCESS_KEY"))
	}
	if cfg.SecretKey == "" {
		cfg.SecretKey = strings.TrimSpace(os.Getenv("S3_SECRET_KEY"))
	}
	if cfg.Region == "" || cfg.Region == "us-east-1" {
		if r := strings.TrimSpace(os.Getenv("AWS_REGION")); r != "" {
			cfg.Region = r
		}
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "migrate-storage: "+msg)
	os.Exit(1)
}
