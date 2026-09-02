package questions

import "time"

// Question mirrors the questions table.
type Question struct {
	ID                  string    `json:"id"`
	DocumentID          string    `json:"document_id"`
	QuestionNumber      *string   `json:"question_number"`
	QuestionText        *string   `json:"question_text"`
	PageNumber          *int      `json:"page_number"` // legacy, points to start_page
	StartPage           *int      `json:"start_page"`
	EndPage             *int      `json:"end_page"`
	StartOffset         *int      `json:"start_offset"`
	EndOffset           *int      `json:"end_offset"`
	Confidence          *float64  `json:"confidence"`
	QuestionType        *string   `json:"question_type"`
	OptionsJSON         *string   `json:"options_json"`
	ExtractionNotesJSON *string   `json:"extraction_notes_json"`
	ImagesJSON          *string   `json:"images_json"`
	Year                *int      `json:"year"`
	Subject             *string   `json:"subject"`
	Difficulty          *string   `json:"difficulty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}
