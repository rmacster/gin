package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"gin-server/models"

	"github.com/gorilla/mux"
)

// AdminListPending returns users awaiting approval.
func AdminListPending(w http.ResponseWriter, r *http.Request) {
	users, err := models.GetPendingUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load pending users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// AdminListUsers returns every account.
func AdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := models.GetAllUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func pathUserID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		return 0, false
	}
	return id, true
}

// AdminApprove approves a pending user.
func AdminApprove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUserID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := models.ApproveUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// AdminReject deletes a still-pending user.
func AdminReject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUserID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := models.RejectUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not reject user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// AdminDeleteUser removes an approved (non-admin) user.
func AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUserID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := models.DeleteUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AdminCreateUser lets an admin create an already-approved account directly.
func AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if _, err := models.GetUserByUsername(req.Username); err == nil {
		writeError(w, http.StatusConflict, "username already taken")
		return
	}
	u, err := models.CreateUser(req.Username, req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}
	if err := models.ApproveUser(u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve user")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}
