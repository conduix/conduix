package stream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/bcrypt"
)

// EncryptStage encrypts, hashes, or masks specified fields
type EncryptStage struct {
	BaseStage
	fields        []string
	method        string
	keyEnv        string
	maskChar      string
	maskKeepFirst int
	maskKeepLast  int
	targetField   string // optional: store result in different field (only for single field)
	truncate      int    // optional: truncate result to N chars (0 = no truncation)
}

// NewEncryptStage creates a new encrypt stage from config
func NewEncryptStage(name string, config map[string]any) (*EncryptStage, error) {
	s := &EncryptStage{
		BaseStage:     BaseStage{name: name, typ: "encrypt", config: config},
		method:        "sha256",
		maskChar:      "*",
		maskKeepFirst: 0,
		maskKeepLast:  4,
	}

	// Parse fields (comes as []any from JSON/YAML)
	if f, ok := config["fields"].([]any); ok {
		for _, v := range f {
			if str, ok := v.(string); ok {
				s.fields = append(s.fields, str)
			}
		}
	}
	if len(s.fields) == 0 {
		return nil, fmt.Errorf("encrypt stage requires at least one field")
	}

	if m, ok := config["method"].(string); ok {
		s.method = m
	}
	if k, ok := config["key_env"].(string); ok {
		s.keyEnv = k
	}
	if mc, ok := config["mask_char"].(string); ok && mc != "" {
		s.maskChar = mc
	}
	if mkf, ok := config["mask_keep_first"].(float64); ok {
		s.maskKeepFirst = int(mkf)
	}
	if mkf, ok := config["mask_keep_first"].(int); ok {
		s.maskKeepFirst = mkf
	}
	if mkl, ok := config["mask_keep_last"].(float64); ok {
		s.maskKeepLast = int(mkl)
	}
	if mkl, ok := config["mask_keep_last"].(int); ok {
		s.maskKeepLast = mkl
	}

	if tf, ok := config["target_field"].(string); ok {
		s.targetField = tf
	}
	if tr, ok := config["truncate"].(float64); ok && tr > 0 {
		s.truncate = int(tr)
	}
	if tr, ok := config["truncate"].(int); ok && tr > 0 {
		s.truncate = tr
	}

	// Validate method
	switch s.method {
	case "sha1", "sha256", "sha512", "bcrypt", "mask", "aes256":
		// valid
	default:
		return nil, fmt.Errorf("unsupported encrypt method: %s", s.method)
	}

	return s, nil
}

// Process applies encryption/hashing/masking to the specified fields
func (s *EncryptStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	for _, field := range s.fields {
		val, ok := record.Data[field]
		if !ok {
			continue
		}

		strVal := fmt.Sprintf("%v", val)

		encrypted, err := s.transform(strVal)
		if err != nil {
			s.incrementError()
			return nil, fmt.Errorf("encrypt field %q with method %q: %w", field, s.method, err)
		}

		if s.truncate > 0 && len(encrypted) > s.truncate {
			encrypted = encrypted[:s.truncate]
		}

		if s.targetField != "" && len(s.fields) == 1 {
			record.Data[s.targetField] = encrypted
		} else {
			record.Data[field] = encrypted
		}
	}

	s.incrementOutput()
	return record, nil
}

// transform applies the configured encryption method to a string value
func (s *EncryptStage) transform(value string) (string, error) {
	switch s.method {
	case "sha1":
		h := sha1.Sum([]byte(value))
		return hex.EncodeToString(h[:]), nil

	case "sha256":
		h := sha256.Sum256([]byte(value))
		return hex.EncodeToString(h[:]), nil

	case "sha512":
		h := sha512.Sum512([]byte(value))
		return hex.EncodeToString(h[:]), nil

	case "bcrypt":
		hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if err != nil {
			return "", fmt.Errorf("bcrypt hash failed: %w", err)
		}
		return string(hash), nil

	case "mask":
		return s.applyMask(value), nil

	case "aes256":
		return s.encryptAES256(value)

	default:
		return "", fmt.Errorf("unsupported method: %s", s.method)
	}
}

// applyMask masks characters in the value, keeping first/last N characters
func (s *EncryptStage) applyMask(value string) string {
	runes := []rune(value)
	length := len(runes)

	keepFirst := s.maskKeepFirst
	keepLast := s.maskKeepLast

	// If keep_first + keep_last >= length, return original
	if keepFirst+keepLast >= length {
		return value
	}

	maskRune := []rune(s.maskChar)
	if len(maskRune) == 0 {
		maskRune = []rune("*")
	}

	result := make([]rune, length)
	for i := range length {
		if i < keepFirst || i >= length-keepLast {
			result[i] = runes[i]
		} else {
			result[i] = maskRune[0]
		}
	}
	return string(result)
}

// encryptAES256 encrypts the value using AES-256-GCM
func (s *EncryptStage) encryptAES256(value string) (string, error) {
	keyStr := os.Getenv(s.keyEnv)
	if keyStr == "" {
		return "", fmt.Errorf("encryption key environment variable %q is not set", s.keyEnv)
	}

	// Key must be 32 bytes for AES-256
	key := []byte(keyStr)
	if len(key) != 32 {
		return "", fmt.Errorf("AES-256 key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(value), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
