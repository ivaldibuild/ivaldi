// Package email holds the one rule for what Ivaldi accepts as an email
// address, so every capability that stores one agrees.
package email

import "net/mail"

// MaxLength is the longest address accepted, the limit RFC 5321 sets. Columns
// that hold an address are VARCHAR(320).
const MaxLength = 320

// Valid reports whether s is a bare address such as ada@example.com: no
// display name or angle brackets, and at most MaxLength characters.
func Valid(s string) bool {
	if len(s) > MaxLength {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && addr.Name == ""
}
