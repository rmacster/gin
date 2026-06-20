package ginrummy

import (
	"errors"
	"sync"
)

const (
	PhaseDraw     = "draw"     // current player must draw
	PhaseDiscard  = "discard"  // current player must discard (and may knock)
	PhaseRoundEnd = "roundEnd" // hand finished, results available
	PhaseGameOver = "gameOver" // someone reached the target score
)

const KnockThreshold = 10

var (
	ErrNotYourTurn  = errors.New("it's not your turn yet — wait for the other players to act")
	ErrWrongPhase   = errors.New("you can't do that right now")
	ErrEmptyStock   = errors.New("the stock pile ran out, so this hand is a draw")
	ErrNoCard       = errors.New("you don't have that card in your hand")
	ErrCannotKnock  = errors.New("you can't knock — your deadwood must be 10 or less")
	ErrEmptyDiscard = errors.New("the discard pile is empty — draw from the stock instead")
)

// Player is a seat at the table.
type Player struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	IsRobot  bool   `json:"is_robot"`
	Hand     []Card `json:"-"`
	Score    int    `json:"score"`
	Connected bool  `json:"connected"`
}

// HandResult captures one player's standing when a hand ends.
type HandResult struct {
	UserID   int      `json:"user_id"`
	Username string   `json:"username"`
	Melds    []Meld   `json:"melds"`
	Deadwood int      `json:"deadwood"`
	Points   int      `json:"points"`     // points scored this hand
	IsKnocker bool    `json:"is_knocker"`
	Gin      bool     `json:"gin"`
	Undercut bool     `json:"undercut"`
	HandCodes []string `json:"hand"`
}

// Game is a single gin rummy match (best-of to TargetScore).
type Game struct {
	ID          int       `json:"id"`
	Players     []*Player `json:"players"`
	Stock       []Card    `json:"-"`
	DiscardPile []Card    `json:"-"`
	Turn        int       `json:"turn"`         // index into Players
	Phase       string    `json:"phase"`
	HandNumber  int       `json:"hand_number"`
	DealerIdx   int       `json:"dealer_idx"`
	TargetScore int       `json:"target_score"`
	HandSize    int       `json:"hand_size"`
	WinnerID    int       `json:"winner_id"` // 0 until game over
	LastResults []HandResult `json:"-"`
	LastDrawFrom string   `json:"-"` // "stock" or "discard" of the most recent draw, for UI hints
	mu          sync.Mutex
}

// NewGame creates and deals the first hand. players must have UserID/Username/IsRobot set.
func NewGame(id int, players []*Player, target int) *Game {
	g := &Game{
		ID:          id,
		Players:     players,
		TargetScore: target,
		DealerIdx:   0,
	}
	if len(players) >= 3 {
		g.HandSize = 7
	} else {
		g.HandSize = 10
	}
	g.deal()
	return g
}

// Lock/Unlock expose the game mutex for the caller (handlers serialize a game).
func (g *Game) Lock()   { g.mu.Lock() }
func (g *Game) Unlock() { g.mu.Unlock() }

func (g *Game) deal() {
	deck := NewDeck()
	Shuffle(deck)
	for _, p := range g.Players {
		p.Hand = nil
	}
	pos := 0
	for r := 0; r < g.HandSize; r++ {
		for _, p := range g.Players {
			p.Hand = append(p.Hand, deck[pos])
			pos++
		}
	}
	for _, p := range g.Players {
		sortCards(p.Hand)
	}
	g.DiscardPile = []Card{deck[pos]}
	pos++
	g.Stock = deck[pos:]
	g.Turn = (g.DealerIdx + 1) % len(g.Players) // player left of dealer starts
	g.Phase = PhaseDraw
	g.HandNumber++
	g.LastDrawFrom = ""
}

func (g *Game) playerIndex(userID int) int {
	for i, p := range g.Players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}

// CurrentPlayer returns the player whose turn it is (nil if round/game over).
func (g *Game) CurrentPlayer() *Player {
	if g.Phase != PhaseDraw && g.Phase != PhaseDiscard {
		return nil
	}
	return g.Players[g.Turn]
}

// Draw takes the top of the stock or the discard pile for the current player.
func (g *Game) Draw(userID int, fromDiscard bool) (Card, error) {
	idx := g.playerIndex(userID)
	if idx != g.Turn {
		return 0, ErrNotYourTurn
	}
	if g.Phase != PhaseDraw {
		return 0, ErrWrongPhase
	}
	p := g.Players[idx]
	var c Card
	if fromDiscard {
		if len(g.DiscardPile) == 0 {
			return 0, ErrEmptyDiscard
		}
		c = g.DiscardPile[len(g.DiscardPile)-1]
		g.DiscardPile = g.DiscardPile[:len(g.DiscardPile)-1]
		g.LastDrawFrom = "discard"
	} else {
		if len(g.Stock) == 0 {
			// Stock exhausted with no knock: the hand is a draw.
			g.endHandWashed()
			return 0, ErrEmptyStock
		}
		c = g.Stock[len(g.Stock)-1]
		g.Stock = g.Stock[:len(g.Stock)-1]
		g.LastDrawFrom = "stock"
	}
	p.Hand = append(p.Hand, c)
	sortCards(p.Hand)
	g.Phase = PhaseDiscard
	return c, nil
}

// Discard plays a card from the current player's hand. If knock is true the
// player attempts to knock/gin; the hand then ends and is scored.
func (g *Game) Discard(userID int, card Card, knock bool) error {
	idx := g.playerIndex(userID)
	if idx != g.Turn {
		return ErrNotYourTurn
	}
	if g.Phase != PhaseDiscard {
		return ErrWrongPhase
	}
	p := g.Players[idx]
	pos := -1
	for i, c := range p.Hand {
		if c == card {
			pos = i
			break
		}
	}
	if pos < 0 {
		return ErrNoCard
	}

	if knock {
		// Evaluate the hand WITHOUT the discard.
		remaining := append([]Card{}, p.Hand[:pos]...)
		remaining = append(remaining, p.Hand[pos+1:]...)
		a := Analyze(remaining)
		if a.Deadwood > KnockThreshold {
			return ErrCannotKnock
		}
	}

	// Remove the discard from hand and place it on the pile.
	p.Hand = append(p.Hand[:pos], p.Hand[pos+1:]...)
	g.DiscardPile = append(g.DiscardPile, card)

	if knock {
		g.scoreHand(idx)
		return nil
	}

	// Advance to the next player.
	g.Turn = (g.Turn + 1) % len(g.Players)
	g.Phase = PhaseDraw
	g.LastDrawFrom = ""
	return nil
}

func (g *Game) endHandWashed() {
	g.LastResults = nil
	for _, p := range g.Players {
		a := Analyze(p.Hand)
		g.LastResults = append(g.LastResults, HandResult{
			UserID: p.UserID, Username: p.Username,
			Melds: a.Melds, Deadwood: a.Deadwood, Points: 0,
			HandCodes: codes(p.Hand),
		})
	}
	g.afterHand()
}

// scoreHand computes results for a knock/gin by player at knockerIdx.
func (g *Game) scoreHand(knockerIdx int) {
	knocker := g.Players[knockerIdx]
	ka := Analyze(knocker.Hand)
	gin := ka.Deadwood == 0

	results := make([]HandResult, len(g.Players))
	for i, p := range g.Players {
		a := Analyze(p.Hand)
		results[i] = HandResult{
			UserID: p.UserID, Username: p.Username,
			Melds: a.Melds, Deadwood: a.Deadwood,
			HandCodes: codes(p.Hand),
		}
	}
	results[knockerIdx].IsKnocker = true
	results[knockerIdx].Gin = gin

	knockerGain := 0
	for i, p := range g.Players {
		if i == knockerIdx {
			continue
		}
		oppDead := results[i].Deadwood
		if !gin {
			// Opponents may lay off onto the knocker's melds.
			oppDead, _ = LayOff(Analyze(p.Hand).Unmatched, ka.Melds)
			results[i].Deadwood = oppDead
		}
		if gin {
			// Gin: knocker collects each opponent's full deadwood; no undercut.
			knockerGain += oppDead
		} else if oppDead > ka.Deadwood {
			knockerGain += oppDead - ka.Deadwood
		} else {
			// Undercut: this opponent scores the difference plus a 25 bonus.
			pts := (ka.Deadwood - oppDead) + 25
			results[i].Points += pts
			results[i].Undercut = true
			g.Players[i].Score += pts
		}
	}
	if gin {
		knockerGain += 25 // gin bonus (once)
	}
	results[knockerIdx].Points = knockerGain
	knocker.Score += knockerGain

	g.LastResults = results
	g.afterHand()
}

func (g *Game) afterHand() {
	// Check for a match winner.
	winner, best := 0, -1
	reached := false
	for _, p := range g.Players {
		if p.Score >= g.TargetScore {
			reached = true
		}
		if p.Score > best {
			best = p.Score
			winner = p.UserID
		}
	}
	if reached {
		g.WinnerID = winner
		g.Phase = PhaseGameOver
		return
	}
	g.Phase = PhaseRoundEnd
}

// NextHand deals a fresh hand after a round has ended, rotating the dealer.
func (g *Game) NextHand() error {
	if g.Phase != PhaseRoundEnd {
		return ErrWrongPhase
	}
	g.DealerIdx = (g.DealerIdx + 1) % len(g.Players)
	g.deal()
	return nil
}

// DiscardTop returns the current top discard card and whether one exists.
func (g *Game) DiscardTop() (Card, bool) {
	if len(g.DiscardPile) == 0 {
		return 0, false
	}
	return g.DiscardPile[len(g.DiscardPile)-1], true
}
