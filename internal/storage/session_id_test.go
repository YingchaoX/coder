package storage

import (
	"regexp"
	"testing"
)

var sessIDRe = regexp.MustCompile(`^sess_\d+_[0-9a-f]+$`)

func TestNewSessionID(t *testing.T) {
	id := NewSessionID()
	if id == "" {
		t.Fatal("NewSessionID returned empty")
	}
	if !sessIDRe.MatchString(id) {
		t.Fatalf("NewSessionID format unexpected: %q", id)
	}
	// Uniqueness in quick succession
	id2 := NewSessionID()
	if id == id2 {
		t.Fatal("NewSessionID should produce different ids")
	}
}
