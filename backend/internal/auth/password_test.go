package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", DefaultArgon2idParams)
	if err != nil {
		t.Fatal(err)
	}
	valid, needsUpgrade, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !valid || needsUpgrade {
		t.Fatalf("VerifyPassword() = valid %v, needsUpgrade %v, err %v", valid, needsUpgrade, err)
	}
	valid, _, err = VerifyPassword("wrong password", hash)
	if err != nil || valid {
		t.Fatalf("wrong password accepted: valid %v, err %v", valid, err)
	}
}

func TestPasswordHashReadsStoredParameters(t *testing.T) {
	params := Argon2idParams{TimeCost: 1, MemoryCostKiB: 32768, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := HashPassword("password", params)
	if err != nil {
		t.Fatal(err)
	}
	valid, needsUpgrade, err := VerifyPassword("password", hash)
	if err != nil || !valid || !needsUpgrade {
		t.Fatalf("stored parameters were not preserved: valid %v, needsUpgrade %v, err %v", valid, needsUpgrade, err)
	}
}

func TestMalformedPasswordHashFailsSafely(t *testing.T) {
	valid, _, err := VerifyPassword("password", "not-a-hash")
	if err == nil || valid {
		t.Fatalf("malformed hash result = valid %v, err %v", valid, err)
	}
}
