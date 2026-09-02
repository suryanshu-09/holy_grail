package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// handleDocumentImages serves extracted images for a document.
// Route: GET /api/v1/documents/{id}/images/{name}
func handleDocumentImages(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		id := r.PathValue("id")
		name := r.PathValue("name")
		if id == "" || name == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id or image name")
			return
		}
		if !isSafeSegment(id) {
			httpx.Error(w, http.StatusBadRequest, "invalid document id")
			return
		}
		if !isSafeImageName(name) {
			httpx.Error(w, http.StatusBadRequest, "invalid image name")
			return
		}
		absRoot, err := filepath.Abs(dataDir)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		// Construct path: <root>/documents/<id>/images/<name>
		dest := filepath.Join(absRoot, "documents", id, "images", name)
		if !isWithinRoot(absRoot, dest) {
			httpx.Error(w, http.StatusBadRequest, "invalid path")
			return
		}
		if _, err := os.Stat(dest); err != nil {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		// Detect content type by extension
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".tiff", ".tif":
			w.Header().Set("Content-Type", "image/tiff")
		case ".jp2":
			w.Header().Set("Content-Type", "image/jp2")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		// Cache for 1 hour
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, r, dest)
	}
}

// handleListDocumentImages lists extracted images for a document.
// Route: GET /api/v1/documents/{id}/images
func handleListDocumentImages(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id")
			return
		}
		if !isSafeSegment(id) {
			httpx.Error(w, http.StatusBadRequest, "invalid document id")
			return
		}
		absRoot, err := filepath.Abs(dataDir)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		dir := filepath.Join(absRoot, "documents", id, "images")
		if !isWithinRoot(absRoot, dir) {
			httpx.Error(w, http.StatusBadRequest, "invalid path")
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				httpx.WriteJSON(w, http.StatusOK, []string{})
				return
			}
			httpx.Error(w, http.StatusInternalServerError, "failed to list images")
			return
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Exclude thumbnails from listing? Keep both but allow filter query param
			if strings.HasPrefix(name, "thumb_") && r.URL.Query().Get("thumbs") != "1" {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		// Also try to enrich with metadata from extraction pages.json if available
		// For now return simple names; frontend can construct URLs.
		// Attempt to read pages.json to get positions if available
		imagesWithMeta := make([]map[string]interface{}, 0, len(names))
		for _, n := range names {
			imagesWithMeta = append(imagesWithMeta, map[string]interface{}{
				"name": n,
				"url":  "/api/v1/documents/" + id + "/images/" + n,
			})
		}
		// If client expects raw list of strings, support both? Return objects for richer UI.
		if r.URL.Query().Get("simple") == "1" {
			httpx.WriteJSON(w, http.StatusOK, names)
			return
		}
		// Try to return enriched JSON with positions from pages.json
		if data, err := os.ReadFile(filepath.Join(absRoot, "documents", id, "extraction", "pages.json")); err == nil {
			var doc struct {
				Pages []struct {
					Images []map[string]interface{} `json:"images"`
				} `json:"pages"`
			}
			if json.Unmarshal(data, &doc) == nil {
				// Log but not needed
				_ = doc
			}
		}
		httpx.WriteJSON(w, http.StatusOK, imagesWithMeta)
	}
}

func isSafeSegment(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`) && !strings.Contains(id, "..")
}

func isSafeImageName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return false
	}
	// Allow only alphanum, dash, underscore, dot
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func isWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
