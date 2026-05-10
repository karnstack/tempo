package web

// UserDTO is the canonical JSON shape for a user row in API responses. The
// password hash is intentionally never serialised.
type UserDTO struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
