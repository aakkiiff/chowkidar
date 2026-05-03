package api

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-for-unit-tests"

func TestGenerateToken_Parseable(t *testing.T) {
	token, err := GenerateToken("alice", RoleAdmin, testSecret)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	// Should be parseable with jwt library
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("token not parseable: %v", err)
	}
}

func TestValidateToken_ValidToken(t *testing.T) {
	token, err := GenerateToken("bob", RoleDeveloper, testSecret)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Username != "bob" {
		t.Errorf("expected username bob, got %s", claims.Username)
	}
	if claims.Role != RoleDeveloper {
		t.Errorf("expected role developer, got %s", claims.Role)
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	// Build a token with a past expiry manually.
	claims := Claims{
		Username: "expired-user",
		Role:     RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = ValidateToken(token, testSecret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, err := GenerateToken("alice", RoleAdmin, testSecret)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	_, err = ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateToken_AlgNone(t *testing.T) {
	// Craft a token using alg:none — the library won't sign it so we build raw.
	claims := Claims{
		Username: "attacker",
		Role:     RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	// jwt library rejects UnsafeAllowNoneSignatureType unless explicitly allowed;
	// but we still test our ValidateToken rejects such a token if somehow presented.
	_, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Build a none-signed token string manually: header.payload.
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1c2VybmFtZSI6ImF0dGFja2VyIiwicm9sZSI6ImFkbWluIn0."
	_, err = ValidateToken(noneToken, testSecret)
	if err == nil {
		t.Fatal("expected error for alg:none token, got nil")
	}
}

func TestValidateToken_EmptyRoleRejected(t *testing.T) {
	claims := Claims{
		Username: "legacy-user",
		Role:     "",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = ValidateToken(token, testSecret)
	if err == nil {
		t.Fatal("expected error for empty role claim, got nil")
	}
}
