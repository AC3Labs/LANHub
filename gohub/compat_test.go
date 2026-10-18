package main

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Vectors produced by the PHP hub's own libraries (Illuminate Encrypter,
// PragmaRX Google2FA, password_hash) with a throwaway key, not the app key.
const testKey = "base64:ERERERERERERERERERERESIiIiIiIiIiIiIiIiIiIiI="

func TestDecryptsValuesEncryptedByLaravel(t *testing.T) {
	e, err := NewEncrypter(testKey)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"eyJpdiI6IktUSmJkSWdLTmlPTUJ5c1BCVGRacFE9PSIsInZhbHVlIjoiVEtRTXBDRVpSZStReVc0VmRXeVFYaThDWGJ6TWJEamJhZTBCNFppTVJQRT0iLCJtYWMiOiI4ODUwMzRlNTVmMTlhNzcyMGEzZTY5NmI3YjkzNmRiMjVmM2VjN2Q0NDIzOWJmOGE4M2JkMzVmNTJjYmQ1NjNhIiwidGFnIjoiIn0=": "test-agent-token-1234567890",
		"eyJpdiI6IkdPZUc4MGhPOVgvQjdTaUpDZXFUS0E9PSIsInZhbHVlIjoielA3SFlMWUJReldMOTRhYVFyRi9zdkJzQkgvVG1lTWFZMkMzVzZnT3IyQT0iLCJtYWMiOiIyNmY4YmVhZGFmNDY1YTA0MTJiMjhlNWI2YzdjNDM4NTM1NmIzYmVkZThmNDBlNDBiYmZlYjUzYWQwN2E3N2RiIiwidGFnIjoiIn0=": `["AAAAA-BBBBB","CCCCC-DDDDD"]`,
		"eyJpdiI6IjFjM3plSXZFbEFYQ29UcFVmN2xWMnc9PSIsInZhbHVlIjoiTm1hcHV1a2c0bmd6WVhNdURsaWJUQT09IiwibWFjIjoiMThlYThkOGIyZGY0OGY0NGQxMWEzNGY2NTAwZjBkZmE5OGJmYzdjNmE3NjBiNzQ3MTdmZjAzZTlkOTYyNGY3MSIsInRhZyI6IiJ9":                                                         "pässwörd ✓",
	}
	for enc, want := range cases {
		got, err := e.DecryptString(enc)
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
}

func TestEncryptRoundTripsAndRejectsTampering(t *testing.T) {
	e, _ := NewEncrypter(testKey)
	enc, err := e.EncryptString("hello agent")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := e.DecryptString(enc); err != nil || got != "hello agent" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	other, _ := NewEncrypter("base64:MzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzM=")
	if _, err := other.DecryptString(enc); err == nil {
		t.Fatal("a different key must fail the MAC check")
	}
	if _, err := e.DecryptString("bm90IGpzb24="); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestBadAppKeysAreRejected(t *testing.T) {
	for _, k := range []string{"", "plain-text-key", "base64:c2hvcnQ="} {
		if _, err := NewEncrypter(k); err == nil {
			t.Fatalf("%q should be rejected", k)
		}
	}
}

func TestTOTPMatchesGoogle2FA(t *testing.T) {
	for _, v := range []struct {
		t    int64
		code string
	}{{59, "996554"}, {1111111109, "071271"}, {1234567890, "742275"}, {2000000000, "890699"}} {
		got, err := totpCode("JBSWY3DPEHPK3PXP", v.t/30)
		if err != nil || got != v.code {
			t.Fatalf("t=%d: got %s %v want %s", v.t, got, err, v.code)
		}
	}
}

func TestVerifyTOTPWindowAndReplayGuard(t *testing.T) {
	now := time.Unix(1234567890, 0)
	step := now.Unix() / 30
	code, _ := totpCode("JBSWY3DPEHPK3PXP", step)

	if got := verifyTOTP("JBSWY3DPEHPK3PXP", code, 0, now); got != step {
		t.Fatalf("current code: got %d", got)
	}
	if got := verifyTOTP("JBSWY3DPEHPK3PXP", "742 275", 0, now); got != step {
		t.Fatal("spaces should be ignored")
	}
	// Replay: the same step can't be used twice.
	if got := verifyTOTP("JBSWY3DPEHPK3PXP", code, step, now); got != 0 {
		t.Fatal("a code from an already-used step must be refused")
	}
	// One step of drift either way is accepted, two is not.
	prev, _ := totpCode("JBSWY3DPEHPK3PXP", step-1)
	next, _ := totpCode("JBSWY3DPEHPK3PXP", step+1)
	old, _ := totpCode("JBSWY3DPEHPK3PXP", step-2)
	if verifyTOTP("JBSWY3DPEHPK3PXP", prev, 0, now) != step-1 || verifyTOTP("JBSWY3DPEHPK3PXP", next, 0, now) != step+1 {
		t.Fatal("±1 step should be accepted")
	}
	if verifyTOTP("JBSWY3DPEHPK3PXP", old, 0, now) != 0 {
		t.Fatal("2 steps old should be refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if verifyTOTP("JBSWY3DPEHPK3PXP", bad, 0, now) != 0 {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestRecoveryCodeNormalisation(t *testing.T) {
	for in, want := range map[string]string{"k7mpq 3hxwa": "K7MPQ-3HXWA", "K7MPQ-3HXWA": "K7MPQ-3HXWA", "k7mpq3hxwa": "K7MPQ-3HXWA", "short": "SHORT"} {
		if got := normalizeRecoveryCode(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestReadsPHPBcryptHashes(t *testing.T) {
	hash := "$2y$10$QZ1dXGtGqZYZdQyc7bTtR.5Xd7twk0Q4T56VPTaPmVzDVr3QNaRpi"
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("correct horse")) != nil {
		t.Fatal("PHP's $2y$ hash should verify")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong")) == nil {
		t.Fatal("wrong password must fail")
	}
	h := hashPassword("new password")
	if h[:4] != "$2y$" || !checkPassword(h, "new password") {
		t.Fatalf("new hashes must be $2y$ so the PHP hub can read them: %s", h)
	}
}
