package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"gin-server/ginrummy"
	"gin-server/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// LobbyHub tracks players sitting in the lobby (i.e. not in a game) so the server
// can push real-time invitations to them. A user counts as "available" exactly
// when they hold a live lobby connection here — entering a game closes it.
type LobbyHub struct {
	mu      sync.Mutex
	clients map[int]map[*lobbyClient]bool // userID -> open connections
}

type lobbyClient struct {
	userID int
	conn   *websocket.Conn
	send   chan []byte
}

func NewLobbyHub() *LobbyHub {
	return &LobbyHub{clients: make(map[int]map[*lobbyClient]bool)}
}

func (h *LobbyHub) add(c *lobbyClient) {
	h.mu.Lock()
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = make(map[*lobbyClient]bool)
	}
	h.clients[c.userID][c] = true
	h.mu.Unlock()
}

func (h *LobbyHub) remove(c *lobbyClient) {
	h.mu.Lock()
	if set := h.clients[c.userID]; set != nil {
		if _, ok := set[c]; ok {
			delete(set, c)
			close(c.send)
		}
		if len(set) == 0 {
			delete(h.clients, c.userID)
		}
	}
	h.mu.Unlock()
}

// Online reports whether the user currently has a lobby connection.
func (h *LobbyHub) Online(userID int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients[userID]) > 0
}

// Notify pushes a payload to every one of a user's lobby connections.
func (h *LobbyHub) Notify(userID int, payload []byte) {
	h.mu.Lock()
	conns := make([]*lobbyClient, 0, len(h.clients[userID]))
	for c := range h.clients[userID] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		select {
		case c.send <- payload:
		default: // slow consumer — drop this message rather than block
		}
	}
}

// ServeLobby upgrades a lobby presence connection. It carries no gameplay; its
// only jobs are to mark the player available and to deliver invite pushes.
func (s *Server) ServeLobby(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok {
		username = r.URL.Query().Get("u")
		password = r.URL.Query().Get("p")
	}
	user, ok := Authenticate(username, password)
	if !ok || user.IsAdmin() {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &lobbyClient{userID: user.ID, conn: conn, send: make(chan []byte, 8)}
	s.Lobby.add(c)

	go func() {
		defer conn.Close()
		for msg := range c.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// We don't expect inbound messages; the read loop just detects disconnect.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	s.Lobby.remove(c)
}

type inviteReq struct {
	GameType    string `json:"game_type"`
	TargetScore int    `json:"target_score"`
}

// InviteGroup creates a game seating the host plus every group member who is
// currently available (online in the lobby, hence not already playing), then
// pushes each of them a real-time invite to join.
func (s *Server) InviteGroup(w http.ResponseWriter, r *http.Request) {
	host := UserFrom(r)
	groupID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	if ok, err := models.IsGroupMember(groupID, host.ID); err != nil || !ok {
		writeError(w, http.StatusForbidden, "you are not a member of that group")
		return
	}
	var req inviteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetScore <= 0 {
		req.TargetScore = 100
	}
	if req.GameType != "rummy" {
		req.GameType = "gin"
	}
	maxPlayers := 3 // Gin: 2–3 players
	if req.GameType == "rummy" {
		maxPlayers = 4 // Standard Rummy: 2–4 players
	}

	members, err := models.GetGroupMembers(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load group members")
		return
	}

	players := []*ginrummy.Player{{UserID: host.ID, Username: host.Username}}
	var invited []*models.User
	for _, m := range members {
		if m.ID == host.ID || m.IsAdmin() || !m.Approved {
			continue
		}
		if !s.Lobby.Online(m.ID) {
			continue // not currently available
		}
		if len(players) >= maxPlayers {
			break // table is full
		}
		players = append(players, &ginrummy.Player{UserID: m.ID, Username: m.Username})
		invited = append(invited, m)
	}
	if len(invited) == 0 {
		writeError(w, http.StatusConflict, "no one else in this group is available right now")
		return
	}

	gameID, err := s.launchGame(&groupID, req.GameType, req.TargetScore, players)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create game")
		return
	}

	groupName := ""
	if g, err := models.GetGroupByID(groupID); err == nil && g != nil {
		groupName = g.Name
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"type": "invite", "game_id": gameID, "game_type": req.GameType,
		"from": host.Username, "group": groupName,
	})
	for _, m := range invited {
		s.Lobby.Notify(m.ID, payload)
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"game_id": gameID, "invited": len(invited)})
}

// DeclineInvite removes the caller from a game they were invited to. The hand is
// re-dealt to whoever remains; if fewer than two are left, the game is cancelled
// and the remaining players are sent back to the lobby.
func (s *Server) DeclineInvite(w http.ResponseWriter, r *http.Request) {
	user := UserFrom(r)
	gameID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid game id")
		return
	}
	rm := s.Hub.Room(gameID)
	if rm == nil {
		writeError(w, http.StatusNotFound, "game not found or no longer live")
		return
	}
	if ok, err := models.IsGamePlayer(gameID, user.ID); err != nil || !ok {
		writeError(w, http.StatusForbidden, "you are not a player in this game")
		return
	}

	rm.game.Lock()
	remaining, removed := rm.game.RemovePlayer(user.ID)
	rm.game.Unlock()
	if !removed {
		writeError(w, http.StatusConflict, "you are no longer in this game")
		return
	}
	_ = models.RemoveGamePlayer(gameID, user.ID) // drop the seat row either way

	if remaining < 2 {
		rm.closeWithMessage(user.Username + " declined — not enough players, so the game was cancelled.")
		s.Hub.Remove(gameID)
		_ = models.DeleteGame(gameID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
		return
	}
	rm.logMove(user.ID, user.Username, "declined the invite and left")
	rm.broadcastState()
	rm.persist()
	rm.AdvanceRobots() // the re-deal may hand the turn to a robot
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}
