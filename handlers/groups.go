package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"gin-server/models"

	"github.com/gorilla/mux"
)

// ListPlayers returns the approved players (people the user can invite).
func ListPlayers(w http.ResponseWriter, r *http.Request) {
	users, err := models.GetActivePlayers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load players")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// ListGroups returns the groups the current user belongs to.
func ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := models.GetGroupsForUser(UserFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load groups")
		return
	}
	type groupOut struct {
		*models.Group
		Members []*models.User `json:"members"`
	}
	out := make([]groupOut, 0, len(groups))
	for _, g := range groups {
		members, _ := models.GetGroupMembers(g.ID)
		out = append(out, groupOut{Group: g, Members: members})
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateGroup makes a new group owned by the current user.
func CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "group name is required")
		return
	}
	g, err := models.CreateGroup(req.Name, UserFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create group")
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// AddGroupMember adds a player to a group the current user belongs to.
func AddGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	member, err := models.IsGroupMember(groupID, UserFrom(r).ID)
	if err != nil || !member {
		writeError(w, http.StatusForbidden, "you are not a member of this group")
		return
	}
	var req struct {
		UserID int `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := models.AddGroupMember(groupID, req.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not add member")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}
