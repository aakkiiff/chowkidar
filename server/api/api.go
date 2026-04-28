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
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
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
		// Default any pre-role token to admin so existing sessions don't lose access.
		if claims.Role == "" {
			claims.Role = RoleAdmin
		}
		return claims, nil
	}
	return nil, jwt.ErrSignatureInvalid
}
