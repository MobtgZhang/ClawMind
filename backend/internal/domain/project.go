package domain

import "time"

// Project groups chat sessions (ChatGPT-style sidebar).
type Project struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
