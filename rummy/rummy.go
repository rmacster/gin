// Package rummy implements Standard (Basic) Rummy: players draw, lay melds
// face-up on the table, lay off onto existing melds, and win a hand by emptying
// their hand. It reuses the card and meld primitives from the ginrummy package.
package rummy

import (
	"errors"
	"strings"
	"sync"

	gr "gin-server/ginrummy"
)

const (
	PhaseDraw     = "draw"     // current player must draw
	PhasePlay     = "play"     // may meld / lay off, must discard to end the turn
	PhaseRoundEnd = "roundEnd" // a player went out; results available
	PhaseGameOver = "gameOver" // someone reached the target score
)

var (
	errNotYourTurn  = errors.New("It's not your turn yet — wait for the other players to act.")
	errAlreadyDrew  = errors.New("You've already drawn — meld, lay off, or discard to finish your turn.")
	errDrawFirst    = errors.New("Draw a card before you meld, lay off, or discard.")
	errEmptyDiscard = errors.New("The discard pile is empty — draw from the stock instead.")
	errBadMeld      = errors.New("Those cards don't form a valid set or run (3+ of a kind, or 3+ in a sequence of one suit).")
	errBadLayoff    = errors.New("That card can't be added to that meld.")
	errNoCard       = errors.New("You don't have that card in your hand.")
	errBadCard      = errors.New("That isn't a valid card.")
	errNotRoundEnd  = errors.New("The hand isn't over yet — finish the current hand first.")
	errBadAction    = errors.New("That action isn't allowed.")
	// errHandEnded is an internal signal (not an illegal-move error) meaning the
	// draw could not happen because the hand ended (stalemate / no cards left).
	errHandEnded = errors.New("the hand has ended")
)

// TableMeld is a meld laid face-up on the table; anyone may lay off onto it.
type TableMeld struct {
	Kind  string    `json:"kind"`
	Cards []gr.Card `json:"-"`
	Codes []string  `json:"cards"`
	Owner int       `json:"owner"`
}

func (tm *TableMeld) asMeld() gr.Meld {
	return gr.Meld{Kind: tm.Kind, Cards: tm.Cards, Codes: tm.Codes}
}

// HandResult is one player's standing when a hand ends.
type HandResult struct {
	UserID    int      `json:"user_id"`
	Username  string   `json:"username"`
	WentOut   bool     `json:"went_out"`
	Blocked   bool     `json:"blocked"`   // won a blocked (stalemate) hand on lowest count
	Remaining int      `json:"remaining"` // total value of cards left in hand
	Points    int      `json:"points"`    // points scored this hand
	HandCodes []string `json:"hand"`
}

// RummyGame is a single Standard Rummy match (to TargetScore).
type RummyGame struct {
	ID          int
	Players     []*gr.Player
	Stock       []gr.Card
	DiscardPile []gr.Card
	Table       []*TableMeld
	Turn        int
	Phase       string
	HandNumber  int
	DealerIdx   int
	TargetScore int
	HandSize    int
	WinnerID    int
	LastResults []HandResult
	progressed  bool // a meld or lay-off happened since the last stock reshuffle
	reshuffles  int  // how many times the discard has been recycled into the stock
	mu          sync.Mutex
}

// NewGame creates and deals the first hand. Each player gets 7 cards.
func NewGame(id int, players []*gr.Player, target int) *RummyGame {
	g := &RummyGame{ID: id, Players: players, TargetScore: target, DealerIdx: 0, HandSize: 7}
	g.deal()
	return g
}

func (g *RummyGame) Lock()   { g.mu.Lock() }
func (g *RummyGame) Unlock() { g.mu.Unlock() }

func (g *RummyGame) deal() {
	deck := gr.NewDeck()
	gr.Shuffle(deck)
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
		gr.SortCards(p.Hand)
	}
	g.DiscardPile = []gr.Card{deck[pos]}
	pos++
	g.Stock = deck[pos:]
	g.Table = nil
	g.Turn = (g.DealerIdx + 1) % len(g.Players)
	g.Phase = PhaseDraw
	g.HandNumber++
	g.LastResults = nil
	g.progressed = false
	g.reshuffles = 0
}

func (g *RummyGame) playerIndex(userID int) int {
	for i, p := range g.Players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}

func (g *RummyGame) handHas(idx int, c gr.Card) bool {
	for _, x := range g.Players[idx].Hand {
		if x == c {
			return true
		}
	}
	return false
}

// cardsFromHand parses codes and verifies each (distinct) card is held.
func (g *RummyGame) cardsFromHand(idx int, codes []string) ([]gr.Card, error) {
	seen := map[gr.Card]bool{}
	var cards []gr.Card
	for _, code := range codes {
		c, err := gr.ParseCard(code)
		if err != nil {
			return nil, errBadCard
		}
		if seen[c] || !g.handHas(idx, c) {
			return nil, errNoCard
		}
		seen[c] = true
		cards = append(cards, c)
	}
	return cards, nil
}

func (g *RummyGame) removeFromHand(idx int, cards []gr.Card) {
	drop := map[gr.Card]bool{}
	for _, c := range cards {
		drop[c] = true
	}
	hand := g.Players[idx].Hand[:0]
	for _, c := range g.Players[idx].Hand {
		if !drop[c] {
			hand = append(hand, c)
		}
	}
	g.Players[idx].Hand = hand
}

// refillStock turns the discard pile (minus its top) back into a fresh stock.
// It returns false — having ended the hand via endBlockedHand — when the deck
// has fully cycled with no meld or lay-off since the previous reshuffle, which
// means no further play is possible (a stalemate), or there is nothing to
// reshuffle.
func (g *RummyGame) refillStock() bool {
	if len(g.DiscardPile) <= 1 || (g.reshuffles >= 1 && !g.progressed) {
		g.endBlockedHand()
		return false
	}
	g.reshuffles++
	g.progressed = false
	top := g.DiscardPile[len(g.DiscardPile)-1]
	rest := append([]gr.Card{}, g.DiscardPile[:len(g.DiscardPile)-1]...)
	gr.Shuffle(rest)
	g.Stock = rest
	g.DiscardPile = []gr.Card{top}
	return true
}

// endBlockedHand ends a deadlocked hand: the player holding the fewest points'
// worth of cards wins and collects the others' remaining card values. A tie for
// lowest washes the hand with no score.
func (g *RummyGame) endBlockedHand() {
	vals := make([]int, len(g.Players))
	best, winner, ties := 1<<30, -1, 0
	for i, p := range g.Players {
		v := 0
		for _, c := range p.Hand {
			v += c.Value()
		}
		vals[i] = v
		if v < best {
			best, winner, ties = v, i, 1
		} else if v == best {
			ties++
		}
	}
	if winner < 0 || ties > 1 {
		g.washHand()
		return
	}
	results := make([]HandResult, len(g.Players))
	total := 0
	for i, p := range g.Players {
		results[i] = HandResult{UserID: p.UserID, Username: p.Username, Remaining: vals[i], HandCodes: gr.Codes(p.Hand)}
		if i != winner {
			total += vals[i]
		}
	}
	results[winner].Blocked = true
	results[winner].Points = total
	g.Players[winner].Score += total
	g.LastResults = results
	g.afterHand()
}

// Draw takes the top of the stock or discard for the current player.
func (g *RummyGame) Draw(userID int, fromDiscard bool) (gr.Card, error) {
	idx := g.playerIndex(userID)
	if idx != g.Turn {
		return 0, errNotYourTurn
	}
	if g.Phase != PhaseDraw {
		return 0, errAlreadyDrew
	}
	p := g.Players[idx]
	var c gr.Card
	if fromDiscard {
		if len(g.DiscardPile) == 0 {
			return 0, errEmptyDiscard
		}
		c = g.DiscardPile[len(g.DiscardPile)-1]
		g.DiscardPile = g.DiscardPile[:len(g.DiscardPile)-1]
	} else {
		if len(g.Stock) == 0 && !g.refillStock() {
			// The hand ended (stalemate / no cards left); refillStock scored it.
			return 0, errHandEnded
		}
		c = g.Stock[len(g.Stock)-1]
		g.Stock = g.Stock[:len(g.Stock)-1]
	}
	p.Hand = append(p.Hand, c)
	gr.SortCards(p.Hand)
	g.Phase = PhasePlay
	return c, nil
}

// Meld lays a valid set or run from the player's hand onto the table.
func (g *RummyGame) Meld(userID int, codes []string) (cards []gr.Card, wentOut bool, err error) {
	idx := g.playerIndex(userID)
	if idx != g.Turn {
		return nil, false, errNotYourTurn
	}
	if g.Phase != PhasePlay {
		return nil, false, errDrawFirst
	}
	cards, err = g.cardsFromHand(idx, codes)
	if err != nil {
		return nil, false, err
	}
	kind, ok := gr.ValidMeld(cards)
	if !ok {
		return nil, false, errBadMeld
	}
	g.removeFromHand(idx, cards)
	gr.SortCards(cards)
	g.Table = append(g.Table, &TableMeld{Kind: kind, Cards: cards, Codes: gr.Codes(cards), Owner: userID})
	g.progressed = true
	if len(g.Players[idx].Hand) == 0 {
		g.goOut(idx)
		wentOut = true
	}
	return cards, wentOut, nil
}

// Layoff adds one hand card to an existing table meld.
func (g *RummyGame) Layoff(userID int, code string, meldIdx int) (card gr.Card, wentOut bool, err error) {
	idx := g.playerIndex(userID)
	if idx != g.Turn {
		return 0, false, errNotYourTurn
	}
	if g.Phase != PhasePlay {
		return 0, false, errDrawFirst
	}
	if meldIdx < 0 || meldIdx >= len(g.Table) {
		return 0, false, errBadLayoff
	}
	card, err = gr.ParseCard(code)
	if err != nil {
		return 0, false, errBadCard
	}
	if !g.handHas(idx, card) {
		return 0, false, errNoCard
	}
	tm := g.Table[meldIdx]
	grown, ok := gr.CanLayOff(tm.asMeld(), card)
	if !ok {
		return 0, false, errBadLayoff
	}
	g.removeFromHand(idx, []gr.Card{card})
	tm.Cards = grown
	tm.Codes = gr.Codes(grown)
	g.progressed = true
	if len(g.Players[idx].Hand) == 0 {
		g.goOut(idx)
		wentOut = true
	}
	return card, wentOut, nil
}

// Discard plays a card to the discard pile, ending the turn (or going out).
func (g *RummyGame) Discard(userID int, code string) (card gr.Card, wentOut bool, err error) {
	idx := g.playerIndex(userID)
	if idx != g.Turn {
		return 0, false, errNotYourTurn
	}
	if g.Phase != PhasePlay {
		return 0, false, errDrawFirst
	}
	card, err = gr.ParseCard(code)
	if err != nil {
		return 0, false, errBadCard
	}
	if !g.handHas(idx, card) {
		return 0, false, errNoCard
	}
	g.removeFromHand(idx, []gr.Card{card})
	g.DiscardPile = append(g.DiscardPile, card)
	if len(g.Players[idx].Hand) == 0 {
		g.goOut(idx)
		return card, true, nil
	}
	g.Turn = (g.Turn + 1) % len(g.Players)
	g.Phase = PhaseDraw
	return card, false, nil
}

// goOut scores the hand: the player who emptied their hand collects the total
// value of every opponent's remaining cards.
func (g *RummyGame) goOut(winnerIdx int) {
	results := make([]HandResult, len(g.Players))
	total := 0
	for i, p := range g.Players {
		rem := 0
		for _, c := range p.Hand {
			rem += c.Value()
		}
		results[i] = HandResult{UserID: p.UserID, Username: p.Username, Remaining: rem, HandCodes: gr.Codes(p.Hand)}
		if i != winnerIdx {
			total += rem
		}
	}
	results[winnerIdx].WentOut = true
	results[winnerIdx].Points = total
	g.Players[winnerIdx].Score += total
	g.LastResults = results
	g.afterHand()
}

// washHand ends a hand with no winner (stock and discard exhausted).
func (g *RummyGame) washHand() {
	results := make([]HandResult, len(g.Players))
	for i, p := range g.Players {
		rem := 0
		for _, c := range p.Hand {
			rem += c.Value()
		}
		results[i] = HandResult{UserID: p.UserID, Username: p.Username, Remaining: rem, HandCodes: gr.Codes(p.Hand)}
	}
	g.LastResults = results
	g.afterHand()
}

func (g *RummyGame) afterHand() {
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

// NextHand deals a fresh hand after a round ended, rotating the dealer.
func (g *RummyGame) NextHand() error {
	if g.Phase != PhaseRoundEnd {
		return errNotRoundEnd
	}
	g.DealerIdx = (g.DealerIdx + 1) % len(g.Players)
	g.deal()
	return nil
}

// DiscardTop returns the current top discard card, if any.
func (g *RummyGame) DiscardTop() (gr.Card, bool) {
	if len(g.DiscardPile) == 0 {
		return 0, false
	}
	return g.DiscardPile[len(g.DiscardPile)-1], true
}

// describeMeld renders a meld for the move log, e.g. "a run (4H, 5H, 6H)".
func describeMeld(cards []gr.Card) string {
	kind, ok := gr.ValidMeld(cards)
	if !ok {
		kind = "meld"
	}
	return "a " + kind + " (" + strings.Join(gr.Codes(cards), ", ") + ")"
}

// --- engine interface methods ---

func (g *RummyGame) TurnUserID() int {
	if g.Phase == PhaseDraw || g.Phase == PhasePlay {
		return g.Players[g.Turn].UserID
	}
	return 0
}

func (g *RummyGame) TurnIsRobot() bool {
	return (g.Phase == PhaseDraw || g.Phase == PhasePlay) && g.Players[g.Turn].IsRobot
}

func (g *RummyGame) GameOver() bool { return g.Phase == PhaseGameOver }
func (g *RummyGame) Winner() int    { return g.WinnerID }

func (g *RummyGame) Seats() []gr.Seat {
	seats := make([]gr.Seat, 0, len(g.Players))
	for _, p := range g.Players {
		seats = append(seats, gr.Seat{UserID: p.UserID, IsRobot: p.IsRobot})
	}
	return seats
}

func (g *RummyGame) SetConnected(userID int, connected bool) {
	for _, p := range g.Players {
		if p.UserID == userID {
			p.Connected = connected
		}
	}
}

// Apply performs a human action and returns past-tense log verbs. Caller holds the lock.
func (g *RummyGame) Apply(userID int, a gr.Action) ([]string, error) {
	switch a.Type {
	case "draw":
		fromDiscard := a.From == "discard"
		c, err := g.Draw(userID, fromDiscard)
		if err == errHandEnded {
			return []string{"reached for the stock, but the deck has cycled with no plays left — the hand is blocked"}, nil
		}
		if err != nil {
			return nil, err
		}
		if fromDiscard {
			return []string{"drew the " + c.Name() + " from the discard"}, nil
		}
		return []string{"drew from the stock"}, nil

	case "meld":
		cards, wentOut, err := g.Meld(userID, a.Cards)
		if err != nil {
			return nil, err
		}
		return outVerbs("laid down "+describeMeld(cards), wentOut), nil

	case "layoff":
		card, wentOut, err := g.Layoff(userID, a.Card, a.MeldIdx)
		if err != nil {
			return nil, err
		}
		return outVerbs("laid off the "+card.Name(), wentOut), nil

	case "discard":
		card, wentOut, err := g.Discard(userID, a.Card)
		if err != nil {
			return nil, err
		}
		return outVerbs("discarded the "+card.Name(), wentOut), nil

	case "nextHand":
		if err := g.NextHand(); err != nil {
			return nil, errNotRoundEnd
		}
		return []string{"dealt the next hand"}, nil

	case "forceDraw":
		// Consensual draw (all humans agreed): wash the hand with no score.
		if g.Phase != PhaseDraw && g.Phase != PhasePlay {
			return nil, errBadAction
		}
		g.washHand()
		return []string{"agreed to a draw — the hand washes with no score"}, nil

	default:
		return nil, errBadAction
	}
}

func outVerbs(verb string, wentOut bool) []string {
	if wentOut {
		return []string{verb, "went out — Rummy!"}
	}
	return []string{verb}
}
