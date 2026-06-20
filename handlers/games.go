package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"gin-server/ginrummy"
	"gin-server/models"
	"gin-server/rummy"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Server bundles the live-game Hub with the HTTP handlers that need it.
type Server struct {
	Hub *Hub
}

func NewServer() *Server {
	return &Server{Hub: NewHub()}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Local app: accept any origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

type createGameReq struct {
	Opponents   []int  `json:"opponents"`    // user ids of human opponents
	Robots      int    `json:"robots"`       // number of robot opponents
	TargetScore int    `json:"target_score"` // points to win the match
	GroupID     *int   `json:"group_id"`     // optional group context
	GameType    string `json:"game_type"`    // "gin" | "rummy"
}

// CreateGame starts a new game seating the creator, chosen opponents and robots.
func (s *Server) CreateGame(w http.ResponseWriter, r *http.Request) {
	creator := UserFrom(r)
	var req createGameReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetScore <= 0 {
		req.TargetScore = 100
	}
	if req.Robots < 0 {
		req.Robots = 0
	}
	if req.GameType != "rummy" {
		req.GameType = "gin"
	}

	humans := 1 + len(req.Opponents)
	total := humans + req.Robots
	maxPlayers := 3 // Gin: 2–3 players
	if req.GameType == "rummy" {
		maxPlayers = 4 // Standard Rummy: 2–4 players
	}
	if total < 2 || total > maxPlayers {
		writeError(w, http.StatusBadRequest,
			"this game must have 2 to "+strconv.Itoa(maxPlayers)+" players total (humans + robots)")
		return
	}

	// Validate the optional group: creator and all opponents must belong to it.
	if req.GroupID != nil {
		if ok, err := models.IsGroupMember(*req.GroupID, creator.ID); err != nil || !ok {
			writeError(w, http.StatusForbidden, "you are not a member of that group")
			return
		}
	}

	players := []*ginrummy.Player{{UserID: creator.ID, Username: creator.Username}}
	seen := map[int]bool{creator.ID: true}
	for _, oid := range req.Opponents {
		if seen[oid] {
			writeError(w, http.StatusBadRequest, "duplicate opponent")
			return
		}
		opp, err := models.GetUserByID(oid)
		if err != nil || !opp.Approved || opp.IsAdmin() {
			writeError(w, http.StatusBadRequest, "invalid opponent")
			return
		}
		if req.GroupID != nil {
			if ok, err := models.IsGroupMember(*req.GroupID, oid); err != nil || !ok {
				writeError(w, http.StatusBadRequest, "opponent is not in that group")
				return
			}
		}
		seen[oid] = true
		players = append(players, &ginrummy.Player{UserID: opp.ID, Username: opp.Username})
	}

	// Add robot opponents with unique cartoon names and negative ids.
	taken := map[string]bool{}
	for i := 0; i < req.Robots; i++ {
		name := ginrummy.RandomRobotName(taken)
		taken[name] = true
		players = append(players, &ginrummy.Player{UserID: -(i + 1), Username: name, IsRobot: true})
	}

	// Persist the game and its human seats.
	gameID, err := models.CreateGameRow(req.GroupID, req.GameType, req.TargetScore)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create game")
		return
	}
	for seat, p := range players {
		if p.IsRobot {
			continue
		}
		if err := models.AddGamePlayer(gameID, p.UserID, seat); err != nil {
			writeError(w, http.StatusInternalServerError, "could not seat players")
			return
		}
	}

	var game engine
	if req.GameType == "rummy" {
		game = rummy.NewGame(gameID, players, req.TargetScore)
	} else {
		game = ginrummy.NewGame(gameID, players, req.TargetScore)
	}
	rm := s.Hub.AddGame(gameID, game)
	rm.persist()       // save initial state so the game is resumable immediately
	rm.AdvanceRobots() // in case the first player to act is a robot

	writeJSON(w, http.StatusCreated, map[string]interface{}{"game_id": gameID})
}

// ClearGame removes a finished game from the player's list (and the database).
func (s *Server) ClearGame(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid game id")
		return
	}
	ok, err := models.IsGamePlayer(id, UserFrom(r).ID)
	if err != nil || !ok {
		writeError(w, http.StatusForbidden, "that isn't one of your games")
		return
	}
	s.Hub.Remove(id) // evict the live room (if any) so an in-progress game can be cleared too
	if err := models.DeleteGame(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not clear game")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// RestoreGames reloads every active, persisted game into the hub on startup so
// in-progress games survive a server restart and can be resumed from the lobby.
func (s *Server) RestoreGames() {
	games, err := models.GetActiveGames()
	if err != nil {
		log.Printf("restore games: %v", err)
		return
	}
	restored := 0
	for _, g := range games {
		var e engine
		var lerr error
		if g.GameType == "rummy" {
			var rg *rummy.RummyGame
			rg, lerr = rummy.LoadGame(g.State)
			e = rg
		} else {
			var gg *ginrummy.Game
			gg, lerr = ginrummy.LoadGame(g.State)
			e = gg
		}
		if lerr != nil {
			log.Printf("restore game %d: %v", g.ID, lerr)
			continue
		}
		rm := s.Hub.AddGame(g.ID, e)
		rm.AdvanceRobots() // resume any robot whose turn it was
		restored++
	}
	if restored > 0 {
		log.Printf("restored %d active game(s) from the database", restored)
	}
}

// ListGames returns the current user's games.
func (s *Server) ListGames(w http.ResponseWriter, r *http.Request) {
	games, err := models.GetGamesForUser(UserFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load games")
		return
	}
	type gameOut struct {
		*models.GameRow
		Live bool `json:"live"`
	}
	out := make([]gameOut, 0, len(games))
	for _, g := range games {
		out = append(out, gameOut{GameRow: g, Live: s.Hub.Room(g.ID) != nil})
	}
	writeJSON(w, http.StatusOK, out)
}

// wsMessage is an action sent by a client over the WebSocket.
type wsMessage struct {
	Type    string   `json:"type"`     // draw | discard | nextHand | meld | layoff | offerDraw | chat
	From    string   `json:"from"`     // for draw: "stock" | "discard"
	Card    string   `json:"card"`     // for discard / layoff
	Knock   bool     `json:"knock"`    // gin only
	Cards   []string `json:"cards"`    // rummy: cards forming a meld
	MeldIdx int      `json:"meld_idx"` // rummy: target table meld for a layoff
	Text    string   `json:"text"`     // for chat
}

// ServeWS upgrades the connection and joins the player to their game room.
func (s *Server) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Authenticate via Basic Auth header or ?u=&p= query params (browsers cannot
	// set headers on a WebSocket handshake).
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

	gameID, err := strconv.Atoi(r.URL.Query().Get("game"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid game id")
		return
	}
	rm := s.Hub.Room(gameID)
	if rm == nil {
		writeError(w, http.StatusNotFound, "game not found or no longer live")
		return
	}

	// The user must be seated in this game.
	seated := false
	rm.game.Lock()
	for _, st := range rm.game.Seats() {
		if st.UserID == user.ID {
			seated = true
			break
		}
	}
	rm.game.Unlock()
	if !seated {
		writeError(w, http.StatusForbidden, "you are not a player in this game")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Client{room: rm, conn: conn, userID: user.ID, username: user.Username, send: make(chan []byte, 16)}
	rm.register(c)
	go c.writePump()
	c.readPump(s)
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *Client) readPump(s *Server) {
	rm := c.room
	defer rm.unregister(c)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			rm.sendError(c, "malformed message")
			continue
		}
		rm.handle(c, msg)
	}
}

// handle applies one client action to the game. Chat is handled here; every game
// move is dispatched generically through the engine's Apply, so the hub is the
// same for Gin and Standard Rummy.
func (rm *Room) handle(c *Client, msg wsMessage) {
	g := rm.game
	if msg.Type == "chat" {
		text := msg.Text
		if len(text) == 0 || len(text) > 500 {
			return
		}
		rm.saveChat(c.userID, text)
		payload, _ := json.Marshal(map[string]interface{}{
			"type": "chat", "from": c.username, "text": text,
			"ts": time.Now().Format("15:04"),
		})
		rm.broadcast(payload)
		return
	}

	if msg.Type == "offerDraw" {
		rm.handleDrawOffer(c)
		return
	}

	action := ginrummy.Action{
		Type: msg.Type, From: msg.From, Card: msg.Card, Knock: msg.Knock,
		Cards: msg.Cards, MeldIdx: msg.MeldIdx,
	}
	g.Lock()
	verbs, err := g.Apply(c.userID, action)
	gameOver := g.GameOver()
	g.Unlock()
	if err != nil {
		rm.sendError(c, err.Error())
		rm.broadcastState()
		return
	}
	rm.clearDrawVotes() // a real move changed the position — reset any pending draw votes
	for _, v := range verbs {
		rm.logMove(c.userID, c.username, v)
	}
	if gameOver {
		rm.recordResults()
	}
	rm.broadcastState()
	rm.persist()
	rm.AdvanceRobots()
}
