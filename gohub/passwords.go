package main

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// PHP's password_hash() writes "$2y$"; Go's bcrypt writes "$2a$". They are
// the same algorithm, so new hashes are relabelled "$2y$" to stay readable
// by the PHP hub, and either prefix is accepted when checking.

const bcryptCost = 12 // Laravel's default BCRYPT_ROUNDS

func hashPassword(plain string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		// Only fails for passwords over 72 bytes; callers validate length.
		panic(err)
	}
	return "$2y$" + strings.TrimPrefix(string(h), "$2a$")
}

func checkPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
