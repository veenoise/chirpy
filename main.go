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
	"github.com/veenoise/chirpy/internal/database"
)

// apiConfig holds our application's stateful, in-memory data.
type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
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

// Request and Response schemas for Chirp validation
type chirpParams struct {
	Body   string    `json:"body"`
	UserId uuid.UUID `json:"user_id"`
}

type chirpResponse struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserId    uuid.UUID `json:"user_id"`
}

type userParams struct {
	Email string `json:"email"`
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
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

	// Use RuneCountInString so multi-byte characters/emojis are counted as 1 character
	const maxChirpLength = 140
	if utf8.RuneCountInString(params.Body) > maxChirpLength {
		cfg.respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleaned := cleanBody(params.Body)

	chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   cleaned,
		UserID: params.UserId,
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
		UserId:    chirp.UserID,
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

	user, err := cfg.db.CreateUser(r.Context(), params.Email)
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
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
		return
	}
	defer db.Close()
	dbQueries := database.New(db)

	apiCfg := &apiConfig{db: dbQueries, platform: platform}

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
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerValidateChirp)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Starting server on http://localhost:8080...")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Error starting server: %s\n", err)
	}
}
