package handlers

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"gin-server/database"
	"gin-server/ginrummy"
	"gin-server/models"

	"github.com/gorilla/websocket"
)

// engine is the behaviour the hub needs from a game, satisfied by both the Gin
// and the Standard-Rummy implementations.
type engine interface {
	Lock()
	Unlock()
	StateFor(userID int) map[string]interface{}
	TurnUserID() int
	TurnIsRobot() bool
	GameOver() bool
	Winner() int
	Seats() []ginrummy.Seat
	SetConnected(userID int, connected bool)
	RemovePlayer(userID int) (remaining int, removed bool)
	Apply(userID int, a ginrummy.Action) ([]string, error)
	RobotStep() (actorID int, actorName string, verbs []string, acted bool)
	Snapshot() ([]byte, error)
}

// Hub holds every live game room in memory.
type Hub struct {
	mu    sync.Mutex
	rooms map[int]*Room
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[int]*Room)}
}

// AddGame registers a freshly created game and returns its room.
func (h *Hub) AddGame(gameID int, g engine) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	rm := &Room{gameID: gameID, game: g, hub: h, clients: make(map[*Client]bool), drawVotes: make(map[int]bool)}
	h.rooms[gameID] = rm
	return rm
}

func (h *Hub) Room(gameID int) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[gameID]
}

// Remove drops a game's room from the hub (used when a finished game is cleared).
func (h *Hub) Remove(gameID int) {
	h.mu.Lock()
	delete(h.rooms, gameID)
	h.mu.Unlock()
}

// Room is a live game plus the clients watching it.
type Room struct {
	gameID int
	game   engine
	hub    *Hub

	mu           sync.Mutex
	clients      map[*Client]bool
	robotRunning bool
	recorded     bool
	drawVotes    map[int]bool // human userIDs currently voting to end the hand as a draw
}

// Client is one WebSocket connection belonging to a seated player.
type Client struct {
	room     *Room
	conn     *websocket.Conn
	userID   int
	username string
	send     chan []byte
}

func (rm *Room) register(c *Client) {
	rm.mu.Lock()
	rm.clients[c] = true
	rm.mu.Unlock()
	rm.setConnected(c.userID, true)
	rm.broadcastState()
}

func (rm *Room) unregister(c *Client) {
	rm.mu.Lock()
	if _, ok := rm.clients[c]; ok {
		delete(rm.clients, c)
		close(c.send)
	}
	stillHere := false
	for other := range rm.clients {
		if other.userID == c.userID {
			stillHere = true
			break
		}
	}
	rm.mu.Unlock()
	if !stillHere {
		rm.setConnected(c.userID, false)
		rm.broadcastState()
	}
}

func (rm *Room) setConnected(userID int, connected bool) {
	g := rm.game
	g.Lock()
	g.SetConnected(userID, connected)
	g.Unlock()
}

// broadcastState sends each client its personalized view of the game.
func (rm *Room) broadcastState() {
	rm.mu.Lock()
	clients := make([]*Client, 0, len(rm.clients))
	humans := make(map[int]bool)
	for c := range rm.clients {
		clients = append(clients, c)
		humans[c.userID] = true
	}
	voted := make(map[int]bool, len(rm.drawVotes))
	for uid := range rm.drawVotes {
		voted[uid] = true
	}
	rm.mu.Unlock()

	// Tally draw-offer votes among currently-connected humans only.
	drawCount := 0
	for uid := range humans {
		if voted[uid] {
			drawCount++
		}
	}

	g := rm.game
	for _, c := range clients {
		g.Lock()
		state := g.StateFor(c.userID)
		g.Unlock()
		state["draw_offer_votes"] = drawCount
		state["draw_offer_humans"] = len(humans)
		state["you_offered_draw"] = voted[c.userID]
		payload, err := json.Marshal(map[string]interface{}{"type": "state", "state": state})
		if err != nil {
			continue
		}
		rm.sendTo(c, payload)
	}
}

// handleDrawOffer toggles a player's vote to end the hand as a draw. When every
// currently-connected human has voted, the hand washes with no score.
func (rm *Room) handleDrawOffer(c *Client) {
	g := rm.game
	g.Lock()
	over := g.GameOver()
	g.Unlock()
	if over {
		return
	}

	rm.mu.Lock()
	if rm.drawVotes == nil {
		rm.drawVotes = make(map[int]bool)
	}
	if rm.drawVotes[c.userID] {
		delete(rm.drawVotes, c.userID)
	} else {
		rm.drawVotes[c.userID] = true
	}
	allAgreed := len(rm.clients) > 0
	for cl := range rm.clients {
		if !rm.drawVotes[cl.userID] {
			allAgreed = false
			break
		}
	}
	rm.mu.Unlock()

	if !allAgreed {
		rm.broadcastState() // update the "N/M agreed" tally for everyone
		return
	}

	g.Lock()
	verbs, err := g.Apply(c.userID, ginrummy.Action{Type: "forceDraw"})
	g.Unlock()
	rm.clearDrawVotes()
	if err != nil {
		rm.broadcastState()
		return
	}
	for _, v := range verbs {
		rm.logMove(c.userID, c.username, v)
	}
	rm.broadcastState()
	rm.persist()
}

// clearDrawVotes resets all draw votes — called whenever a real move changes the
// position, so a stale vote can't end a hand that just became playable.
func (rm *Room) clearDrawVotes() {
	rm.mu.Lock()
	rm.drawVotes = make(map[int]bool)
	rm.mu.Unlock()
}

func (rm *Room) sendTo(c *Client, payload []byte) {
	select {
	case c.send <- payload:
	default:
		// Slow consumer: drop the connection.
		rm.mu.Lock()
		if _, ok := rm.clients[c]; ok {
			delete(rm.clients, c)
			close(c.send)
		}
		rm.mu.Unlock()
	}
}

// closeWithMessage tells every connected client the game has been closed so the
// UI can return to the lobby (used when an invite decline leaves too few players).
func (rm *Room) closeWithMessage(reason string) {
	payload, _ := json.Marshal(map[string]interface{}{"type": "closed", "reason": reason})
	rm.broadcast(payload)
}

func (rm *Room) broadcast(payload []byte) {
	rm.mu.Lock()
	clients := make([]*Client, 0, len(rm.clients))
	for c := range rm.clients {
		clients = append(clients, c)
	}
	rm.mu.Unlock()
	for _, c := range clients {
		rm.sendTo(c, payload)
	}
}

func (rm *Room) sendError(c *Client, msg string) {
	payload, _ := json.Marshal(map[string]string{"type": "error", "error": msg})
	rm.sendTo(c, payload)
}

// logMove broadcasts a move-log entry. Each client renders the actor as "You"
// when actorID matches them, otherwise by name; verb is past-tense so it reads
// correctly either way ("You drew…" / "Bender drew…").
func (rm *Room) logMove(actorID int, actor, verb string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"type":     "log",
		"actor_id": actorID,
		"actor":    actor,
		"verb":     verb,
		"ts":       time.Now().Format("15:04"),
	})
	rm.broadcast(payload)
}

// AdvanceRobots plays out any robot turns until it is a human's turn or the
// hand/game ends. Safe to call concurrently; only one runner executes at a time.
func (rm *Room) AdvanceRobots() {
	rm.mu.Lock()
	if rm.robotRunning {
		rm.mu.Unlock()
		return
	}
	rm.robotRunning = true
	rm.mu.Unlock()

	go func() {
		defer func() {
			rm.mu.Lock()
			rm.robotRunning = false
			rm.mu.Unlock()
		}()
		g := rm.game
		for {
			g.Lock()
			actorID, actorName, verbs, acted := g.RobotStep()
			gameOver := g.GameOver()
			g.Unlock()
			if !acted {
				return
			}
			for _, v := range verbs {
				rm.logMove(actorID, actorName, v)
			}
			if gameOver {
				rm.recordResults()
			}
			rm.broadcastState()
			rm.persist()
			time.Sleep(750 * time.Millisecond)
		}
	}()
}

// recordResults persists the winner and updates human players' stats, once.
func (rm *Room) recordResults() {
	rm.mu.Lock()
	if rm.recorded {
		rm.mu.Unlock()
		return
	}
	rm.recorded = true
	rm.mu.Unlock()

	g := rm.game
	g.Lock()
	winner := g.Winner()
	seats := g.Seats()
	g.Unlock()

	if err := models.FinishGame(rm.gameID, winner); err != nil {
		log.Printf("finish game %d: %v", rm.gameID, err)
	}
	for _, s := range seats {
		if s.IsRobot {
			continue
		}
		if err := models.RecordResult(s.UserID, s.UserID == winner); err != nil {
			log.Printf("record result for user %d: %v", s.UserID, err)
		}
	}
}

// persist saves the live engine state so the game survives a server restart.
func (rm *Room) persist() {
	g := rm.game
	g.Lock()
	data, err := g.Snapshot()
	g.Unlock()
	if err != nil {
		log.Printf("snapshot game %d: %v", rm.gameID, err)
		return
	}
	if err := models.SaveGameState(rm.gameID, data); err != nil {
		log.Printf("save game state %d: %v", rm.gameID, err)
	}
}

// saveChat persists a chat line scoped to this game.
func (rm *Room) saveChat(fromUserID int, content string) {
	if _, err := database.DB.Exec(
		"INSERT INTO messages (from_user_id, game_id, content) VALUES (?, ?, ?)",
		fromUserID, rm.gameID, content,
	); err != nil {
		log.Printf("save chat: %v", err)
	}
}
