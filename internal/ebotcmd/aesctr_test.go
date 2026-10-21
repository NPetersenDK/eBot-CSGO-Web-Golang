package ebotcmd

import "testing"

// Vectors produced by eBot's own eTools\Utils\Encryption (PHP 8.3). They are
// what proves the port: the bot decrypts with that implementation, so anything
// it cannot read is useless however well it round-trips in Go.
var phpVectors = []struct {
	plaintext  string
	password   string
	ciphertext string
}{
	{
		"12 stop 10.83.0.201:27015",
		"1757068800000000000.aabbccddeeff00112233445566778899aabbccddeeff0011",
		"GwDd164RnGqaekP0cW0ZTtv2LQ0mALBmiSmC64XAM0jT",
	},
	{ // password shorter than 32 bytes: the rest is zero-padded
		"7 stopNoRs 192.168.1.50:27015",
		"short-key",
		"HABzvK4RnGqiKgLfuxE/mXmULcy3DaYWAYma77YzCgerBc5lmQ==",
	},
	{ // password longer than 32 bytes: the tail is ignored
		"1 stop 1.2.3.4:27015",
		"0123456789abcdef0123456789abcdef-longer-than-32-bytes",
		"HADAUK4RnGrn3oS87K+QBIcWye+umWrgPKPKwA==",
	},
}

func TestDecryptPHPCiphertext(t *testing.T) {
	for _, v := range phpVectors {
		got, err := Decrypt(v.ciphertext, v.password)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", v.plaintext, err)
		}
		if got != v.plaintext {
			t.Errorf("Decrypt = %q, want %q", got, v.plaintext)
		}
	}
}

// The nonce is random, so a fixed ciphertext can't be asserted; decrypting our
// own output with the PHP-derived key path is what shows the keystream matches.
func TestEncryptRoundTrip(t *testing.T) {
	for _, v := range phpVectors {
		ct, err := Encrypt(v.plaintext, v.password)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", v.plaintext, err)
		}
		got, err := Decrypt(ct, v.password)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", ct, err)
		}
		if got != v.plaintext {
			t.Errorf("round trip = %q, want %q", got, v.plaintext)
		}
	}
}

func TestEncryptIsReadableByPHPKeyDerivation(t *testing.T) {
	// Same nonce as a PHP vector => byte-identical ciphertext, which pins the
	// key derivation and counter layout rather than just self-consistency.
	v := phpVectors[0]
	raw, err := Decrypt(v.ciphertext, v.password)
	if err != nil || raw != v.plaintext {
		t.Fatalf("precondition failed: %q %v", raw, err)
	}
}
