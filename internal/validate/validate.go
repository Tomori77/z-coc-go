package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var (
	userIDRe    = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	roomIDRe    = regexp.MustCompile(`^[A-Z0-9_-]{6,16}$`)
	senderIDRe  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	ownerTokenRe = regexp.MustCompile(`(?i)^[a-f0-9]{32,128}$`)
)

// UserID normalizes and validates a user id, matching app_normalize_user_id().
func UserID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || !userIDRe.MatchString(value) {
		return "", false
	}
	return value, true
}

// RoomID upper-cases, trims and validates a room id, matching normalize_room_id().
func RoomID(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !roomIDRe.MatchString(value) {
		return "", false
	}
	return value, true
}

// SenderID trims and validates a sender id, matching normalize_sender_id().
func SenderID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || !senderIDRe.MatchString(value) {
		return "", false
	}
	return value, true
}

// OwnerTokenHash validates an owner token and returns its sha256 hash of the
// lower-cased token, matching library_owner_hash().
func OwnerTokenHash(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !ownerTokenRe.MatchString(value) {
		return "", false
	}
	return sha256Hex(strings.ToLower(value)), true
}

// OwnerTokenMatches is the non-fatal variant used when deciding whether a
// bearer token could be an owner token (library_can_read_private_module()).
func OwnerTokenMatches(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !ownerTokenRe.MatchString(value) {
		return "", false
	}
	return sha256Hex(strings.ToLower(value)), true
}
