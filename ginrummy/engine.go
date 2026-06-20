package ginrummy

import (
	"errors"
	"sort"
)

// Action is a client move, shared across game variants. Not every field applies
// to every variant.
type Action struct {
	Type    string   `json:"type"`     // draw | discard | nextHand | meld | layoff
	From    string   `json:"from"`     // draw: "stock" | "discard"
	Card    string   `json:"card"`     // discard / layoff card code
	Knock   bool     `json:"knock"`    // gin only
	Cards   []string `json:"cards"`    // rummy: cards forming a meld
	MeldIdx int      `json:"meld_idx"` // rummy: target table meld for a layoff
}

// Seat identifies a player for stat-recording purposes.
type Seat struct {
	UserID  int
	IsRobot bool
}

// --- ginrummy.Game implements the handler's engine interface ---

// TurnUserID is the user whose turn it is, or 0 if the hand/game is over.
func (g *Game) TurnUserID() int {
	if cp := g.CurrentPlayer(); cp != nil {
		return cp.UserID
	}
	return 0
}

// TurnIsRobot reports whether the current player is a robot.
func (g *Game) TurnIsRobot() bool {
	cp := g.CurrentPlayer()
	return cp != nil && cp.IsRobot
}

func (g *Game) GameOver() bool { return g.Phase == PhaseGameOver }
func (g *Game) Winner() int    { return g.WinnerID }

// Seats returns one Seat per player.
func (g *Game) Seats() []Seat {
	seats := make([]Seat, 0, len(g.Players))
	for _, p := range g.Players {
		seats = append(seats, Seat{UserID: p.UserID, IsRobot: p.IsRobot})
	}
	return seats
}

// RemovePlayer drops a seated player (e.g. an invitee who declined) and re-deals
// a fresh hand to whoever remains — removing a hand mid-deal would corrupt the
// deck. Returns the remaining player count and whether the user was seated; the
// caller closes the game when fewer than 2 remain. Caller holds the lock.
func (g *Game) RemovePlayer(userID int) (remaining int, removed bool) {
	idx := -1
	for i, p := range g.Players {
		if p.UserID == userID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return len(g.Players), false
	}
	g.Players = append(g.Players[:idx], g.Players[idx+1:]...)
	remaining = len(g.Players)
	if remaining < 2 {
		return remaining, true // not playable — caller cancels the game
	}
	if remaining >= 3 { // hand size follows the player count (NewGame's rule)
		g.HandSize = 7
	} else {
		g.HandSize = 10
	}
	g.DealerIdx = 0
	g.LastResults = nil
	g.deal()
	return remaining, true
}

// SetConnected updates a player's connection flag.
func (g *Game) SetConnected(userID int, connected bool) {
	for _, p := range g.Players {
		if p.UserID == userID {
			p.Connected = connected
		}
	}
}

// Apply performs a human action and returns past-tense log verbs describing it.
// The caller must hold the game lock.
func (g *Game) Apply(userID int, a Action) ([]string, error) {
	switch a.Type {
	case "draw":
		fromDiscard := a.From == "discard"
		c, err := g.Draw(userID, fromDiscard)
		if err != nil {
			return nil, errors.New(friendlyDrawErr(err))
		}
		if fromDiscard {
			return []string{"drew the " + c.Name() + " from the discard"}, nil
		}
		return []string{"drew from the stock"}, nil

	case "discard":
		card, perr := ParseCard(a.Card)
		if perr != nil {
			return nil, errors.New("That isn't a valid card.")
		}
		if err := g.Discard(userID, card, a.Knock); err != nil {
			return nil, errors.New(friendlyDiscardErr(err))
		}
		verbs := []string{"discarded the " + card.Name()}
		if a.Knock {
			verbs = append(verbs, knockVerb(g, userID))
		}
		return verbs, nil

	case "nextHand":
		if err := g.NextHand(); err != nil {
			return nil, errors.New("The hand isn't over yet — finish the current hand first.")
		}
		return []string{"dealt the next hand"}, nil

	case "forceDraw":
		// Consensual draw (all humans agreed): wash the hand with no score.
		if g.Phase != PhaseDraw && g.Phase != PhaseDiscard {
			return nil, errors.New("The hand is already over.")
		}
		g.endHandWashed()
		return []string{"agreed to a draw — the hand washes with no score"}, nil

	default:
		return nil, errors.New("That action isn't allowed in Gin Rummy.")
	}
}

// RobotStep plays one robot sub-action (a single draw or discard). It returns the
// acting robot, log verbs, and whether a move was made. The caller holds the lock.
func (g *Game) RobotStep() (actorID int, actorName string, verbs []string, acted bool) {
	cp := g.CurrentPlayer()
	if cp == nil || !cp.IsRobot {
		return 0, "", nil, false
	}
	idx := g.Turn
	actorID, actorName = cp.UserID, cp.Username

	if g.Phase == PhaseDraw {
		from := g.DecideDraw(idx)
		c, err := g.Draw(actorID, from)
		if err != nil {
			// Stock exhausted: the hand washed out. Report it so the UI updates.
			return actorID, actorName, []string{"found the stock empty — the hand is a draw"}, true
		}
		if from {
			return actorID, actorName, []string{"drew the " + c.Name() + " from the discard"}, true
		}
		return actorID, actorName, []string{"drew from the stock"}, true
	}

	card, knock := g.DecideDiscard(idx)
	if err := g.Discard(actorID, card, knock); err != nil {
		return actorID, actorName, nil, false
	}
	verbs = []string{"discarded the " + card.Name()}
	if knock {
		verbs = append(verbs, knockVerb(g, actorID))
	}
	return actorID, actorName, verbs, true
}

func knockVerb(g *Game, userID int) string {
	for _, r := range g.LastResults {
		if r.UserID == userID && r.Gin {
			return "went gin!"
		}
	}
	return "knocked"
}

func friendlyDrawErr(err error) string {
	switch {
	case errors.Is(err, ErrWrongPhase):
		return "You've already drawn this turn — now choose a card to discard."
	case errors.Is(err, ErrNotYourTurn):
		return "It's not your turn yet — wait for the other players to act."
	case errors.Is(err, ErrEmptyDiscard):
		return "The discard pile is empty — draw from the stock instead."
	case errors.Is(err, ErrEmptyStock):
		return "The stock pile ran out, so this hand is a draw."
	default:
		return capitalizeErr(err)
	}
}

func friendlyDiscardErr(err error) string {
	switch {
	case errors.Is(err, ErrWrongPhase):
		return "You need to draw a card before you can discard."
	case errors.Is(err, ErrNotYourTurn):
		return "It's not your turn yet — wait for the other players to act."
	case errors.Is(err, ErrNoCard):
		return "You don't have that card in your hand."
	case errors.Is(err, ErrCannotKnock):
		return "You can't knock — your deadwood must be 10 or less. Uncheck Knock and discard normally."
	default:
		return capitalizeErr(err)
	}
}

func capitalizeErr(err error) string {
	s := err.Error()
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r) + "."
}

// --- Exported helpers reused by the standard-rummy engine ---

// Codes returns the two-character codes for a slice of cards.
func Codes(cards []Card) []string { return codes(cards) }

// SortCards sorts cards in place by suit then rank.
func SortCards(cards []Card) { sortCards(cards) }

// CanLayOff reports whether card c can be appended to meld m and returns the
// grown card list if so.
func CanLayOff(m Meld, c Card) ([]Card, bool) { return canLayOff(m, c) }

// ValidMeld reports whether exactly these cards form a valid set or run, and
// returns the meld kind ("set" or "run").
func ValidMeld(cards []Card) (kind string, ok bool) {
	if len(cards) < 3 {
		return "", false
	}
	cs := append([]Card{}, cards...)
	// Set: all the same rank, all distinct suits (so at most 4).
	sameRank := true
	for _, c := range cs[1:] {
		if c.Rank() != cs[0].Rank() {
			sameRank = false
			break
		}
	}
	if sameRank {
		if len(cs) > 4 {
			return "", false
		}
		seen := map[int]bool{}
		for _, c := range cs {
			if seen[c.Suit()] {
				return "", false
			}
			seen[c.Suit()] = true
		}
		return "set", true
	}
	// Run: all the same suit, consecutive distinct ranks.
	suit := cs[0].Suit()
	for _, c := range cs[1:] {
		if c.Suit() != suit {
			return "", false
		}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].Rank() < cs[b].Rank() })
	for i := 1; i < len(cs); i++ {
		if cs[i].Rank() != cs[i-1].Rank()+1 {
			return "", false
		}
	}
	return "run", true
}
