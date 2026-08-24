package questions

import "time"

// Question mirrors the questions table.
type Question struct {
	ID             string    `json:"id"`
	DocumentID     string    `json:"document_id"`
	QuestionNumber *string   `json:"question_number"`
	QuestionText   *string   `json:"question_text"`
	PageNumber     *int      `json:"page_number"`
	Year           *int      `json:"year"`
	Subject        *string   `json:"subject"`
	Difficulty     *string   `json:"difficulty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
