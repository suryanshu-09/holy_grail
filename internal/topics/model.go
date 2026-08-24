package topics

import "time"

// Topic mirrors the topics table.
type Topic struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Subject   *string   `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}
