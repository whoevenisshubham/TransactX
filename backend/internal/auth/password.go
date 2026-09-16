package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2idParams struct {
	TimeCost      uint32
	MemoryCostKiB uint32
	Parallelism   uint8
	SaltLength    uint32
	KeyLength     uint32
}

var DefaultArgon2idParams = Argon2idParams{
	TimeCost: 3, MemoryCostKiB: 65536, Parallelism: 2, SaltLength: 16, KeyLength: 32,
}

func HashPassword(password string, params Argon2idParams) (string, error) {
	if password == "" || params.SaltLength == 0 || params.KeyLength == 0 {
		return "", errors.New("invalid password hashing input")
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.TimeCost, params.MemoryCostKiB, params.Parallelism, params.KeyLength)
	encoded := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.MemoryCostKiB, params.TimeCost, params.Parallelism, encoded.EncodeToString(salt), encoded.EncodeToString(key)), nil
}

func VerifyPassword(password, encodedHash string) (bool, bool, error) {
	params, salt, expected, err := parseHash(encodedHash)
	if err != nil {
		return false, false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.TimeCost, params.MemoryCostKiB, params.Parallelism, uint32(len(expected)))
	valid := subtle.ConstantTimeCompare(actual, expected) == 1
	needsUpgrade := params != DefaultArgon2idParams
	return valid, needsUpgrade, nil
}

func parseHash(encodedHash string) (Argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Argon2idParams{}, nil, nil, errors.New("malformed password hash")
	}
	params := Argon2idParams{}
	for _, field := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(field, "=", 2)
		if len(pair) != 2 {
			return Argon2idParams{}, nil, nil, errors.New("malformed password parameters")
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return Argon2idParams{}, nil, nil, errors.New("malformed password parameters")
		}
		switch pair[0] {
		case "m":
			params.MemoryCostKiB = uint32(value)
		case "t":
			params.TimeCost = uint32(value)
		case "p":
			params.Parallelism = uint8(value)
		default:
			return Argon2idParams{}, nil, nil, errors.New("unknown password parameter")
		}
	}
	if params.TimeCost == 0 || params.MemoryCostKiB == 0 || params.Parallelism == 0 {
		return Argon2idParams{}, nil, nil, errors.New("invalid password parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Argon2idParams{}, nil, nil, errors.New("malformed password salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Argon2idParams{}, nil, nil, errors.New("malformed password key")
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(key))
	return params, salt, key, nil
}
