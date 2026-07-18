package auth_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/veenoise/chirpy/internal/auth"
)

func TestMakeJWT(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		userID      uuid.UUID
		tokenSecret string
		expiresIn   time.Duration
		want        string
		wantErr     bool
	}{
		{
			name:        "Successfully creates a valid token string",
			userID:      uuid.New(),
			tokenSecret: "my-super-secret-key-12345",
			expiresIn:   1 * time.Hour,
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := auth.MakeJWT(tt.userID, tt.tokenSecret, tt.expiresIn)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("MakeJWT() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("MakeJWT() succeeded unexpectedly")
			}

			if got == "" {
				t.Errorf("MakeJWT() returned an empty token string")
			}
		})
	}
}

func TestValidateJWT(t *testing.T) {
	secret := "my-super-secret-key-12345"
	userID := uuid.New()

	// Pre-generate tokens for different testing contexts
	validToken, _ := auth.MakeJWT(userID, secret, 1*time.Hour)
	expiredToken, _ := auth.MakeJWT(userID, secret, -1*time.Hour) // Negative duration makes it expired

	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		tokenString string
		tokenSecret string
		want        uuid.UUID
		wantErr     bool
	}{
		{
			name:        "Valid token parses successfully",
			tokenString: validToken,
			tokenSecret: secret,
			want:        userID,
			wantErr:     false,
		},
		{
			name:        "Expired token returns an error",
			tokenString: expiredToken,
			tokenSecret: secret,
			want:        uuid.Nil,
			wantErr:     true,
		},
		{
			name:        "Token signed with different secret is rejected",
			tokenString: validToken,
			tokenSecret: "completely-wrong-secret-key",
			want:        uuid.Nil,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := auth.ValidateJWT(tt.tokenString, tt.tokenSecret)

			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ValidateJWT() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ValidateJWT() succeeded unexpectedly")
			}

			if got != tt.want {
				t.Errorf("ValidateJWT() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetBearerToken(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer demoToken.hiWilliam")
	noAuth := http.Header{}

	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		headers http.Header
		want    string
		wantErr bool
	}{
		{
			name:    "Valid Bearer Token",
			headers: header,
			want:    "demoToken.hiWilliam",
			wantErr: false,
		},
		{
			name:    "No Authorization Bearer",
			headers: noAuth,
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := auth.GetBearerToken(tt.headers)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("GetBearerToken() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("GetBearerToken() succeeded unexpectedly")
			}

			if got != tt.want {
				t.Errorf("GetBearerToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		password string
		want     string
		wantErr  bool
	}{
		{
			name:     "Valid hash string",
			password: "williamDemo",
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotErr := auth.HashPassword(tt.password)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("HashPassword() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("HashPassword() succeeded unexpectedly")
			}
		})
	}
}
