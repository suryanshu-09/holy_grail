package documents

import "time"

// Document mirrors the documents table. UserID is the owner when set;
// nil marks a legacy/anonymous row visible to everyone.
type Document struct {
	ID               string    `json:"id"`
	Filename         string    `json:"filename"`
	OriginalFilename string    `json:"original_filename"`
	StoragePath      *string   `json:"storage_path"`
	Subject          *string   `json:"subject"`
	Year             *int      `json:"year"`
	Status           string    `json:"status"`
	UserID           *string   `json:"user_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
