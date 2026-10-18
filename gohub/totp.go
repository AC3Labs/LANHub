package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TOTP matching PragmaRX\Google2FA as the PHP hub uses it: RFC 6238,
// SHA-1, 6 digits, 30-second steps, one step of drift either way, and the
// replay guard of verifyKeyNewer: a code is only accepted for a step after
// the last step used (users.two_factor_last_used_step).

var sixDigits = regexp.MustCompile(`^\d{6}$`)

const recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func totpCode(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000), nil
}

// verifyTOTP returns the matched step, or 0 if the code is wrong, reused, or
// malformed. lastUsedStep is 0 when the user has never used a code.
func verifyTOTP(secret, code string, lastUsedStep int64, now time.Time) int64 {
	code = strings.Join(strings.Fields(code), "")
	if !sixDigits.MatchString(code) {
		return 0
	}
	current := now.Unix() / 30
	start := current - 1
	if lastUsedStep+1 > start {
		start = lastUsedStep + 1
	}
	for step := start; step <= current+1; step++ {
		want, err := totpCode(secret, step)
		if err != nil {
			return 0
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step
		}
	}
	return 0
}

// normalizeRecoveryCode mirrors Authenticator::normalizeRecoveryCode:
// uppercase, spaces and dashes ignored, "XXXXX-XXXXX" form.
func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(code)
	code = strings.NewReplacer(" ", "", "\t", "", "-", "").Replace(code)
	if len(code) == 10 {
		return code[:5] + "-" + code[5:]
	}
	return code
}

func randomFrom(alphabet string, n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}
