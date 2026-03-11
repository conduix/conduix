package stream

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestEncryptStage_SHA256(t *testing.T) {
	config := map[string]any{
		"fields": []any{"password"},
		"method": "sha256",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"password": "secret123"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	expected := sha256.Sum256([]byte("secret123"))
	expectedHex := hex.EncodeToString(expected[:])

	if result.Data["password"] != expectedHex {
		t.Errorf("expected %s, got %v", expectedHex, result.Data["password"])
	}
}

func TestEncryptStage_SHA512(t *testing.T) {
	config := map[string]any{
		"fields": []any{"token"},
		"method": "sha512",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"token": "mytoken"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	expected := sha512.Sum512([]byte("mytoken"))
	expectedHex := hex.EncodeToString(expected[:])

	if result.Data["token"] != expectedHex {
		t.Errorf("expected %s, got %v", expectedHex, result.Data["token"])
	}
}

func TestEncryptStage_Bcrypt(t *testing.T) {
	config := map[string]any{
		"fields": []any{"password"},
		"method": "bcrypt",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"password": "secret123"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	hashed, ok := result.Data["password"].(string)
	if !ok {
		t.Fatal("password field is not a string")
	}

	// Verify bcrypt hash is valid
	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte("secret123")); err != nil {
		t.Errorf("bcrypt hash verification failed: %v", err)
	}
}

func TestEncryptStage_Mask_KeepLast(t *testing.T) {
	config := map[string]any{
		"fields":          []any{"phone"},
		"method":          "mask",
		"mask_char":       "*",
		"mask_keep_last":  float64(4),
		"mask_keep_first": float64(0),
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"phone": "1234567890"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	expected := "******7890"
	if result.Data["phone"] != expected {
		t.Errorf("expected %q, got %q", expected, result.Data["phone"])
	}
}

func TestEncryptStage_Mask_KeepFirstAndLast(t *testing.T) {
	config := map[string]any{
		"fields":          []any{"email"},
		"method":          "mask",
		"mask_char":       "#",
		"mask_keep_first": float64(2),
		"mask_keep_last":  float64(3),
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"email": "user@test.com"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// "user@test.com" (13 chars) -> keep first 2 + last 3 = "us########com"
	expected := "us########com"
	if result.Data["email"] != expected {
		t.Errorf("expected %q, got %q", expected, result.Data["email"])
	}
}

func TestEncryptStage_AES256(t *testing.T) {
	// Set a 32-byte key in env
	key := "01234567890123456789012345678901" // exactly 32 bytes
	t.Setenv("TEST_AES_KEY", key)

	config := map[string]any{
		"fields":  []any{"ssn"},
		"method":  "aes256",
		"key_env": "TEST_AES_KEY",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"ssn": "123-45-6789"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	encrypted, ok := result.Data["ssn"].(string)
	if !ok {
		t.Fatal("ssn field is not a string")
	}

	// Should be base64 encoded
	if encrypted == "123-45-6789" {
		t.Error("ssn should be encrypted, not plain text")
	}
	if !strings.ContainsAny(encrypted, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=") {
		t.Error("encrypted value should be base64 encoded")
	}
}

func TestEncryptStage_AES256_MissingKey(t *testing.T) {
	t.Setenv("MISSING_KEY", "")

	config := map[string]any{
		"fields":  []any{"data"},
		"method":  "aes256",
		"key_env": "MISSING_KEY",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"data": "sensitive"}}
	_, err = stage.Process(context.Background(), record)
	if err == nil {
		t.Error("expected error for missing AES key, got nil")
	}
}

func TestEncryptStage_MultipleFields(t *testing.T) {
	config := map[string]any{
		"fields": []any{"password", "ssn"},
		"method": "sha256",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"password": "secret",
		"ssn":      "123-45-6789",
		"name":     "Alice",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// password and ssn should be hashed, name should remain
	if result.Data["password"] == "secret" {
		t.Error("password should be hashed")
	}
	if result.Data["ssn"] == "123-45-6789" {
		t.Error("ssn should be hashed")
	}
	if result.Data["name"] != "Alice" {
		t.Error("name should remain unchanged")
	}
}

func TestEncryptStage_NoFields_Error(t *testing.T) {
	config := map[string]any{
		"method": "sha256",
	}
	_, err := NewEncryptStage("test", config)
	if err == nil {
		t.Error("expected error for missing fields, got nil")
	}
}

func TestEncryptStage_InvalidMethod(t *testing.T) {
	config := map[string]any{
		"fields": []any{"data"},
		"method": "rot13",
	}
	_, err := NewEncryptStage("test", config)
	if err == nil {
		t.Error("expected error for invalid method, got nil")
	}
}

func TestEncryptStage_SkipsMissingField(t *testing.T) {
	config := map[string]any{
		"fields": []any{"nonexistent"},
		"method": "sha256",
	}
	stage, err := NewEncryptStage("test", config)
	if err != nil {
		t.Fatalf("NewEncryptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"other": "value"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should pass through unchanged
	if result.Data["other"] != "value" {
		t.Error("existing field should remain unchanged")
	}
	if _, exists := result.Data["nonexistent"]; exists {
		t.Error("nonexistent field should not be added")
	}
}
