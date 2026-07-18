# Chirpy

A RESTful API backend for a Twitter-like microblogging platform built with Go, PostgreSQL, and sqlc.

## Features

- **User Management** - Registration, login, and profile updates with secure password hashing
- **Chirps** - Create, read, delete microblog posts (max 140 characters)
- **Authentication** - JWT-based auth with refresh token support
- **Content Filtering** - Automatic profanity filtering for chirps
- **Admin Dashboard** - Metrics tracking and user management
- **Webhook Integration** - Polka payment integration for user upgrades

## Tech Stack

- **Language**: Go 1.25
- **Database**: PostgreSQL with sqlc for type-safe queries
- **Authentication**: JWT (golang-jwt) + Argon2id password hashing
- **Dependencies**: godotenv, uuid, pq

## Project Structure

```
chirpy/
├── main.go                 # HTTP server and route handlers
├── internal/
│   ├── auth/              # JWT and password utilities
│   └── database/          # sqlc-generated database queries
├── sql/
│   ├── schema/            # Database migrations
│   └── queries/           # SQL query definitions
├── assets/                # Static frontend files
├── sqlc.yaml              # sqlc configuration
└── .env.example           # Environment variables template
```

## Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DB_URL` | PostgreSQL connection string | `postgres://user:pass@localhost:5432/chirpy?sslmode=disable` |
| `PLATFORM` | Environment mode (`dev` for development) | `dev` |
| `JWT_SECRET` | Secret key for JWT signing | Generated via `openssl rand -base64 64` |
| `POLKA_KEY` | API key for Polka webhook integration | Your Polka API key |

## Getting Started

1. **Clone and setup**:
   ```bash
   git clone <repository-url>
   cd chirpy
   cp .env.example .env
   ```

2. **Configure environment**:
   - Edit `.env` with your PostgreSQL credentials
   - Generate a JWT secret: `openssl rand -base64 64`

3. **Install dependencies**:
   ```bash
   go mod download
   ```

4. **Setup database**:
   ```bash
   # Run migrations using your preferred method
   # Then generate sqlc code:
   sqlc generate
   ```

5. **Run the server**:
   ```bash
   go run .
   ```

   Server starts on `http://localhost:8080`

## API Endpoints

### Health Check
- `GET /api/healthz` - Returns "OK"

### Users
- `POST /api/users` - Create new user
- `PUT /api/users` - Update user (requires auth)
- `POST /api/login` - Login and receive tokens

### Chirps
- `POST /api/chirps` - Create chirp (requires auth, max 140 chars)
- `GET /api/chirps` - List all chirps (optional: `?author_id=`, `?sort=asc|desc`)
- `GET /api/chirps/{chirpID}` - Get single chirp
- `DELETE /api/chirps/{chirpID}` - Delete chirp (owner only)

### Authentication
- `POST /api/refresh` - Refresh access token
- `POST /api/revoke` - Revoke refresh token

### Webhooks
- `POST /api/polka/webhooks` - Handle Polka payment events

### Admin (dev mode only)
- `GET /admin/metrics` - View visit count
- `POST /admin/reset` - Reset all users

## License

MIT
