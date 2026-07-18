package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Database Response
type DatabaseGetRefreshTokenResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	RevokedAt sql.NullTime `json:"revoked_at"`
}

func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

func CheckPasswordHash(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	// Create the Claims
	currentTime := time.Now()
	expireTime := currentTime.Add(expiresIn)

	claims := &jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(expireTime),
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(currentTime),
		Subject:   userID.String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString([]byte(tokenSecret))
	if err != nil {
		return "", err
	}
	return ss, err
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(tokenSecret), nil
	})

	if err != nil {
		return uuid.Nil, err
	}

	if !token.Valid {
		return uuid.Nil, errors.New("invalid token")
	}

	subject, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, err
	}

	userID, err := uuid.Parse(subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid user ID in claims: %w", err)
	}

	return userID, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	bearerToken := strings.Split(headers.Get("Authorization"), " ")
	if len(bearerToken) != 2 {
		return "", errors.New("No Bearer Token Found")
	}

	if bearerToken[0] != "Bearer" {
		return "", errors.New("No Bearer Token Found")
	}

	authToken := bearerToken[1]
	return authToken, nil
}

func MakeRefreshToken() string {
	key := make([]byte, 32)
	rand.Read(key)
	return hex.EncodeToString(key)
}

func ValidateRefreshToken(refreshToken string, databaseResponse DatabaseGetRefreshTokenResponse) (bool, error) {
	if databaseResponse.ExpiresAt.Before(time.Now()) {
		return false, errors.New("Refresh Token Expired")
	}
	if databaseResponse.RevokedAt.Valid && databaseResponse.RevokedAt.Time.Before(time.Now()) {
		return false, errors.New("Refresh Token Revoked")
	}
	return true, nil
}

func GetAPIKey(headers http.Header) (string, error) {
	bearerToken := strings.Split(headers.Get("Authorization"), " ")
	if len(bearerToken) != 2 {
		return "", errors.New("No Bearer Token Found")
	}

	if bearerToken[0] != "ApiKey" {
		return "", errors.New("No Bearer Token Found")
	}

	authToken := bearerToken[1]
	return authToken, nil
}
