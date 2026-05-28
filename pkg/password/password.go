package password

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

const (
	argon2IDVersion = 19

	argon2IDMemory      = 19 * 1024
	argon2IDTime        = 2
	argon2IDParallelism = 1

	saltLength = 16
	keyLength  = 32
)

var ErrInvalidHash = errors.New("invalid password hash")

type argon2IDParams struct {
	memory      uint32
	time        uint32
	parallelism uint8
}

func Hash(plainText string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(plainText), salt, argon2IDTime, argon2IDMemory, argon2IDParallelism, keyLength)
	encoder := base64.RawStdEncoding

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2IDVersion,
		argon2IDMemory,
		argon2IDTime,
		argon2IDParallelism,
		encoder.EncodeToString(salt),
		encoder.EncodeToString(hash),
	), nil
}

func Verify(hashedPassword, plainText string) (bool, error) {
	params, salt, hash, err := decodeArgon2IDHash(hashedPassword)
	if err != nil {
		return false, err
	}

	comparisonHash := argon2.IDKey(
		[]byte(plainText),
		salt,
		params.time,
		params.memory,
		params.parallelism,
		uint32(len(hash)),
	)

	return subtle.ConstantTimeCompare(hash, comparisonHash) == 1, nil
}

func decodeArgon2IDHash(hashedPassword string) (argon2IDParams, []byte, []byte, error) {
	parts := strings.Split(hashedPassword, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2IDParams{}, nil, nil, ErrInvalidHash
	}

	version, ok := strings.CutPrefix(parts[2], "v=")
	if !ok || version != strconv.Itoa(argon2IDVersion) {
		return argon2IDParams{}, nil, nil, ErrInvalidHash
	}

	params, err := decodeArgon2IDParams(parts[3])
	if err != nil {
		return argon2IDParams{}, nil, nil, err
	}

	encoder := base64.RawStdEncoding
	salt, err := encoder.DecodeString(parts[4])
	if err != nil {
		return argon2IDParams{}, nil, nil, fmt.Errorf("%w: decode salt", ErrInvalidHash)
	}
	hash, err := encoder.DecodeString(parts[5])
	if err != nil {
		return argon2IDParams{}, nil, nil, fmt.Errorf("%w: decode hash", ErrInvalidHash)
	}
	if len(salt) == 0 || len(hash) == 0 {
		return argon2IDParams{}, nil, nil, ErrInvalidHash
	}

	return params, salt, hash, nil
}

func decodeArgon2IDParams(encodedParams string) (argon2IDParams, error) {
	parts := strings.Split(encodedParams, ",")
	if len(parts) != 3 {
		return argon2IDParams{}, ErrInvalidHash
	}

	memory, err := decodeUint32Param(parts[0], "m")
	if err != nil {
		return argon2IDParams{}, err
	}
	time, err := decodeUint32Param(parts[1], "t")
	if err != nil {
		return argon2IDParams{}, err
	}
	parallelism, err := decodeUint8Param(parts[2], "p")
	if err != nil {
		return argon2IDParams{}, err
	}
	if memory == 0 || time == 0 || parallelism == 0 {
		return argon2IDParams{}, ErrInvalidHash
	}

	return argon2IDParams{
		memory:      memory,
		time:        time,
		parallelism: parallelism,
	}, nil
}

func decodeUint32Param(encodedParam, name string) (uint32, error) {
	value, ok := strings.CutPrefix(encodedParam, name+"=")
	if !ok {
		return 0, ErrInvalidHash
	}

	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: parse %s", ErrInvalidHash, name)
	}
	return uint32(parsed), nil
}

func decodeUint8Param(encodedParam, name string) (uint8, error) {
	value, ok := strings.CutPrefix(encodedParam, name+"=")
	if !ok {
		return 0, ErrInvalidHash
	}

	parsed, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("%w: parse %s", ErrInvalidHash, name)
	}
	return uint8(parsed), nil
}
