package store

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"strconv"
)

// sfGuardAlgorithm is the only password algorithm the panel accepts.
const sfGuardAlgorithm = "sha1"

// NewSalt returns the hex md5 of a random number in 100000..999999 followed by
// the username, like PluginsfGuardUser::setPassword.
func NewSalt(username string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		panic("store: no randomness: " + err.Error())
	}
	sum := md5.Sum([]byte(strconv.FormatInt(n.Int64()+100000, 10) + username))
	return hex.EncodeToString(sum[:])
}

// HashPassword returns the lowercase hex sha1 of salt followed by the password.
func HashPassword(salt, plain string) string {
	sum := sha1.Sum([]byte(salt + plain))
	return hex.EncodeToString(sum[:])
}

// CheckPassword compares in constant time.
func CheckPassword(salt, plain, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashPassword(salt, plain)), []byte(hash)) == 1
}
