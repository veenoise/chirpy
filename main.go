package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/veenoise/chirpy/internal/auth"
	"github.com/veenoise/chirpy/internal/database"
)

// apiConfig holds our application's stateful, in-memory data.
type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
	jwtSecret      string
}

// =========================================================================
// MIDDLEWARE
// =========================================================================

// middlewareMetricsInc increments the counter on every file server request.
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// =========================================================================
// HANDLERS
// =========================================================================

// handlerMetrics returns the current hit metrics as structured HTML.
func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	hits := cfg.fileserverHits.Load()
	html := fmt.Sprintf(`<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, hits)

	w.Write([]byte(html))
}

// handlerReset resets the hit counter back to 0.
func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		cfg.respondWithError(w, http.StatusForbidden, "Application Not In Development Mode")
		return
	}

	err := cfg.db.DeleteUsers(r.Context())
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Delete All Users Failed")
		return
	}

	cfg.respondWithJSON(w, http.StatusOK, struct {
		Message string `json:"message"`
	}{
		Message: "Delete All Users Success",
	})
}

// =========================================================================
// Structs
// =========================================================================

// Request and Response schemas for Chirp validation
type chirpParams struct {
	Body   string    `json:"body"`
	UserID uuid.UUID `json:"user_id"`
}

type chirpResponse struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

// Request and Response schemas for User
type userParams struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
}

// handlerValidateChirp decodes and validates the chirp length correctly using runes.
func (cfg *apiConfig) handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 50KB to prevent memory exhaustion attacks
	r.Body = http.MaxBytesReader(w, r.Body, 50*1024)

	decoder := json.NewDecoder(r.Body)
	var params chirpParams
	if err := decoder.Decode(&params); err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Get JWT
	authToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, "No Authorization Bearer Token")
		return
	}

	// Validate JWT
	jwtID, err := auth.ValidateJWT(authToken, cfg.jwtSecret)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, "Access Unauthorized")
		return
	}

	params.UserID = jwtID

	// Use RuneCountInString so multi-byte characters/emojis are counted as 1 character
	const maxChirpLength = 140
	if utf8.RuneCountInString(params.Body) > maxChirpLength {
		cfg.respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleaned := cleanBody(params.Body)

	chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   cleaned,
		UserID: params.UserID,
	})

	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Chirp Creation Failed")
		return
	}

	cfg.respondWithJSON(w, http.StatusCreated, chirpResponse{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

// handlerChirps outputs all chirps in database sorted by create_at ASC
func (cfg *apiConfig) handlerChirps(w http.ResponseWriter, r *http.Request) {
	chirps, err := cfg.db.ReadChirps(r.Context())
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Chirps Read Failed")
		return
	}

	// Convert chirps DB response to []chirpResponse
	var response []chirpResponse

	for _, chirp := range chirps {
		response = append(response, chirpResponse{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		})
	}

	cfg.respondWithJSON(w, http.StatusOK, response)
}

// handlerChirp returns the chirp from database by ID
func (cfg *apiConfig) handlerChirp(w http.ResponseWriter, r *http.Request) {
	queryParam := r.PathValue("chirpID")
	chirp, err := cfg.db.ReadChirp(r.Context(), uuid.MustParse(queryParam))

	if err != nil {
		cfg.respondWithError(w, http.StatusNotFound, "Chirp Read Failed")
		return
	}

	cfg.respondWithJSON(w, http.StatusOK, chirpResponse{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

// handlerUsers create a user
func (cfg *apiConfig) handlerUsers(w http.ResponseWriter, r *http.Request) {
	var params userParams
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&params); err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Hash user password
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Error in hashing password")
		return
	}

	user, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
	})

	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "User Creation Failed")
		return
	}

	cfg.respondWithJSON(w, http.StatusCreated, userResponse{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})
}

// handlerUpdateUser Updates user password and/or email
func (cfg *apiConfig) handlerUpdateUser(w http.ResponseWriter, r *http.Request) {
	// Get Access Token
	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Validate and get user_id
	userID, err := auth.ValidateJWT(bearerToken, cfg.jwtSecret)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Get JSON request body
	var params userParams
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&params); err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Hash password
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "auth.HashPassword() Failed")
		return
	}

	// Update user email and password
	updatedUser, err := cfg.db.UpdateUser(r.Context(), database.UpdateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
		ID:             userID,
	})
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "UpdateUser() Failed")
		return
	}

	cfg.respondWithJSON(w, http.StatusOK, userResponse{
		ID:        updatedUser.ID,
		CreatedAt: updatedUser.CreatedAt,
		UpdatedAt: updatedUser.UpdatedAt,
		Email:     updatedUser.Email,
	})
}

func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	var params userParams

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&params); err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Get User from DB
	user, err := cfg.db.ReadUser(r.Context(), params.Email)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, "Email or Password incorrect")
		return
	}

	authorized, err := auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "Auth Hash Checking Failed")
		return
	}

	if !authorized {
		cfg.respondWithError(w, http.StatusUnauthorized, "Email or Password incorrect")
		return
	}

	// Make JTW Token
	newToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Duration(time.Hour))
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "Encountered a problem in auth.MakeJWT()")
		return
	}

	// Make Refresh Token
	newRefreshToken := auth.MakeRefreshToken()

	// Add Tokens to DB
	now := time.Now()
	sixtyDaysLater := now.AddDate(0, 0, 60)
	refreshToDB, err := cfg.db.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token:     newRefreshToken,
		UserID:    user.ID,
		ExpiresAt: sixtyDaysLater,
	})
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "Encountered a problem in db.CreateRefreshToken()")
		return
	}

	cfg.respondWithJSON(w, http.StatusOK, userResponse{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		Token:        newToken,
		RefreshToken: refreshToDB.Token,
	})
}

func (cfg *apiConfig) handlerRefresh(w http.ResponseWriter, r *http.Request) {
	// Get Request Bearer Token
	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Get Refresh Token from db
	dbRefreshToken, err := cfg.db.ReadRefreshToken(r.Context(), bearerToken)
	if err != nil {
		cfg.respondWithError(w, http.StatusNotFound, "Refresh Token not in Database")
		return
	}

	// Validate Refresh Token
	_, err = auth.ValidateRefreshToken(bearerToken, auth.DatabaseGetRefreshTokenResponse{
		Token:     dbRefreshToken.Token,
		ExpiresAt: dbRefreshToken.ExpiresAt,
		RevokedAt: dbRefreshToken.RevokedAt,
	})
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	newAccessToken, err := auth.MakeJWT(dbRefreshToken.UserID, cfg.jwtSecret, time.Duration(time.Hour))
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "Encountered problem making new access token")
		return
	}

	cfg.respondWithJSON(w, http.StatusOK, struct {
		Token string `json:"token"`
	}{
		Token: newAccessToken,
	})
}

func (cfg *apiConfig) handlerRevoke(w http.ResponseWriter, r *http.Request) {
	// Contains Refresh Token
	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	err = cfg.db.UpdateRevokeRefreshToken(r.Context(), bearerToken)
	if err != nil {
		cfg.respondWithError(w, http.StatusNotFound, "Refresh Token not in Database")
	}

	w.WriteHeader(http.StatusNoContent)
}

// =========================================================================
// JSON HELPERS
// =========================================================================

type errorResponse struct {
	Error string `json:"error"`
}

// respondWithError is a helper that wraps respondWithJSON for error responses.
func (cfg *apiConfig) respondWithError(w http.ResponseWriter, code int, msg string) {
	cfg.respondWithJSON(w, code, errorResponse{
		Error: msg,
	})
}

// cleanBody replaces occurrences of bad words with **** (case-insensitive)
func cleanBody(body string) string {
	bannedWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}

	words := strings.Split(body, " ")
	for i, word := range words {
		loweredWord := strings.ToLower(word)
		if _, exists := bannedWords[loweredWord]; exists {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}

// respondWithJSON marshals the payload into JSON and writes it to the response writer.
func (cfg *apiConfig) respondWithJSON(w http.ResponseWriter, code int, payload any) {
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

// =========================================================================
// MAIN APPS
// =========================================================================

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	jwtSecret := os.Getenv("JWT_SECRET")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
		return
	}
	defer db.Close()
	dbQueries := database.New(db)

	if platform == "" {
		log.Fatal("ENV ERROR: platform not set")
		return
	}

	if jwtSecret == "" {
		log.Fatal("ENV ERROR: jwtSecret not set")
		return
	}

	apiCfg := &apiConfig{db: dbQueries, platform: platform, jwtSecret: jwtSecret}

	mux := http.NewServeMux()

	// Static Assets Route with hit counting middleware
	fileServerHandler := http.FileServer(http.Dir("."))
	wrappedFileServer := apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fileServerHandler))
	mux.Handle("/app/", wrappedFileServer)

	// API & Admin Routes
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerMetrics)
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)
	mux.HandleFunc("POST /api/users", apiCfg.handlerUsers)
	mux.HandleFunc("PUT /api/users", apiCfg.handlerUpdateUser)
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerValidateChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.handlerChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.handlerChirp)
	mux.HandleFunc("POST /api/login", apiCfg.handlerLogin)
	mux.HandleFunc("POST /api/refresh", apiCfg.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", apiCfg.handlerRevoke)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Starting server on http://localhost:8080...")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Error starting server: %s\n", err)
	}
}
