// Package ebotcmd speaks eBot's admin-command protocol: the AES-CTR dialect its
// panel and bot share, and the Redis list they exchange it on.
package ebotcmd

import (
	"crypto/aes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"time"
)

// Encrypt reproduces eTools\Utils\Encryption::encrypt (the Chris Veness
// "AES Counter-mode" construction that eBot's panel and bot both use). It is
// not interchangeable with Go's crypto/cipher CTR: the key is derived by
// encrypting the password with itself, and an 8-byte nonce is prepended to the
// ciphertext before base64.
func Encrypt(plaintext, password string) (string, error) {
	block, err := aes.NewCipher(deriveKey(password))
	if err != nil {
		return "", err
	}

	// Nonce layout, per NIST SP800-38A B.2 as the PHP does it: [0:2] little-
	// endian milliseconds, [2:4] random, [4:8] little-endian seconds.
	ms := time.Now().UnixMilli()
	var nonce [8]byte
	binary.LittleEndian.PutUint16(nonce[0:2], uint16(ms%1000))
	if _, err := rand.Read(nonce[2:4]); err != nil {
		return "", err
	}
	binary.LittleEndian.PutUint32(nonce[4:8], uint32(ms/1000))

	pt := []byte(plaintext)
	out := make([]byte, 0, 8+len(pt))
	out = append(out, nonce[:]...)

	var counter, keystream [16]byte
	copy(counter[0:8], nonce[:])
	for b := 0; b*16 < len(pt); b++ {
		binary.BigEndian.PutUint64(counter[8:16], uint64(b))
		block.Encrypt(keystream[:], counter[:])
		chunk := pt[b*16:]
		if len(chunk) > 16 {
			chunk = chunk[:16]
		}
		for i, c := range chunk {
			out = append(out, c^keystream[i])
		}
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt is the inverse, kept so the round trip can be tested.
func Decrypt(ciphertext, password string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	if len(raw) < 8 {
		return "", nil
	}
	block, err := aes.NewCipher(deriveKey(password))
	if err != nil {
		return "", err
	}

	body := raw[8:]
	out := make([]byte, 0, len(body))
	var counter, keystream [16]byte
	copy(counter[0:8], raw[0:8])
	for b := 0; b*16 < len(body); b++ {
		binary.BigEndian.PutUint64(counter[8:16], uint64(b))
		block.Encrypt(keystream[:], counter[:])
		chunk := body[b*16:]
		if len(chunk) > 16 {
			chunk = chunk[:16]
		}
		for i, c := range chunk {
			out = append(out, c^keystream[i])
		}
	}
	return string(out), nil
}

// deriveKey mirrors the PHP: take the password's first 32 bytes (zero-padded),
// use them as an AES-256 key to encrypt their own first block, then repeat that
// 16-byte result to reach 32 bytes.
func deriveKey(password string) []byte {
	var pw [32]byte
	copy(pw[:], password)

	block, err := aes.NewCipher(pw[:])
	if err != nil {
		return pw[:] // unreachable: 32 bytes is a valid AES key size
	}
	key := make([]byte, 32)
	block.Encrypt(key[:16], pw[:16])
	copy(key[16:], key[:16])
	return key
}
