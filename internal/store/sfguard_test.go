package store

import (
	"regexp"
	"testing"
)

// Computed independently with: printf 'abcsecret' | shasum -a 1
const vectorHash = "de0a408ef519cd62e7379039634152874895c50c"

func TestHashPasswordVector(t *testing.T) {
	if got := HashPassword("abc", "secret"); got != vectorHash {
		t.Fatalf("got %s", got)
	}
}

func TestCheckPassword(t *testing.T) {
	if !CheckPassword("abc", "secret", vectorHash) {
		t.Fatal("valid password rejected")
	}
	if CheckPassword("abc", "Secret", vectorHash) || CheckPassword("abd", "secret", vectorHash) || CheckPassword("abc", "secret", "") {
		t.Fatal("invalid password accepted")
	}
}

func TestNewSalt(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{32}$`)
	a, b := NewSalt("admin"), NewSalt("admin")
	if !re.MatchString(a) || !re.MatchString(b) {
		t.Fatalf("bad salt format %q %q", a, b)
	}
	if a == b {
		t.Fatal("salts should differ")
	}
}
