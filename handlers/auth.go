package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"gin-server/models"
)

type ctxKey string

const userCtxKey ctxKey = "user"

// writeJSON sends v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Authenticate validates credentials and returns the user if they are allowed
// to use the API (approved, or an admin).
func Authenticate(username, password string) (*models.User, bool) {
	u, err := models.GetUserByUsername(username)
	if err != nil {
		return nil, false
	}
	if !u.CheckPassword(password) {
		return nil, false
	}
	if !u.Approved && !u.IsAdmin() {
		return nil, false
	}
	return u, true
}

// UserFrom returns the authenticated user stored on the request context.
func UserFrom(r *http.Request) *models.User {
	u, _ := r.Context().Value(userCtxKey).(*models.User)
	return u
}

// withUser attaches a user to the request context.
func withUser(r *http.Request, u *models.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
}

// BasicAuth wraps a handler, requiring valid HTTP Basic Auth credentials.
func BasicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="gin"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		u, ok := Authenticate(username, password)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="gin"`)
			writeError(w, http.StatusUnauthorized, "invalid credentials or account not yet approved")
			return
		}
		next(w, withUser(r, u))
	}
}

// PlayerOnly requires the authenticated user to be a (non-admin) player.
func PlayerOnly(next http.HandlerFunc) http.HandlerFunc {
	return BasicAuth(func(w http.ResponseWriter, r *http.Request) {
		if UserFrom(r).IsAdmin() {
			writeError(w, http.StatusForbidden, "admin accounts cannot play")
			return
		}
		next(w, r)
	})
}

// AdminOnly requires the authenticated user to be an admin.
func AdminOnly(next http.HandlerFunc) http.HandlerFunc {
	return BasicAuth(func(w http.ResponseWriter, r *http.Request) {
		if !UserFrom(r).IsAdmin() {
			writeError(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r)
	})
}

// Register creates a new (unapproved) user account.
func Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if _, err := models.GetUserByUsername(req.Username); err == nil {
		writeError(w, http.StatusConflict, "username already taken")
		return
	}
	u, err := models.CreateUser(req.Username, req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       u.ID,
		"username": u.Username,
		"approved": u.Approved,
		"message":  "Account created. An administrator must approve it before you can log in.",
	})
}

// Me returns the currently authenticated user.
func Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, UserFrom(r))
}
