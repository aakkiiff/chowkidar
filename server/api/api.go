package api

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Role constants. Keep in sync with the frontend Role union.
const (
	RoleAdmin     = "admin"
	RoleDeveloper = "developer"
)

// Session lifetime. Reduced from 24h → 5h to limit blast radius if a token
// leaks (XSS, copy/paste of dev tools, etc). Cookie MaxAge in handlers.go
// must stay in sync with this value.
const SessionTTL = 3 * time.Hour

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func GenerateToken(username, role, secret string) (string, error) {
	claims := Claims{
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(SessionTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ValidateToken(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		// Reject anything that isn't HS256 — prevents alg:none and RSA confusion attacks.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		if claims.Role != RoleAdmin && claims.Role != RoleDeveloper {
			return nil, fmt.Errorf("invalid role claim")
		}
		return claims, nil
	}
	return nil, jwt.ErrSignatureInvalid
}
