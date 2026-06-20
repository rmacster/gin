package rummy

import (
	"testing"

	gr "gin-server/ginrummy"
)

func parse(codes ...string) []gr.Card {
	cards := make([]gr.Card, len(codes))
	for i, c := range codes {
		card, err := gr.ParseCard(c)
		if err != nil {
			panic(err)
		}
		cards[i] = card
	}
	return cards
}

func tableMeld(codes ...string) *TableMeld {
	cards := parse(codes...)
	kind, ok := gr.ValidMeld(cards)
	if !ok {
		panic("invalid test meld")
	}
	gr.SortCards(cards)
	return &TableMeld{Kind: kind, Cards: cards, Codes: gr.Codes(cards)}
}

func newTestGame() *RummyGame {
	return &RummyGame{
		ID:          1,
		Players:     []*gr.Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}},
		TargetScore: 100,
		HandSize:    7,
		Phase:       PhasePlay,
		Turn:        0,
		HandNumber:  1,
	}
}

// TestForceDrawWashesHand confirms a consensual draw ends the hand with no score
// change and lands in roundEnd so the next hand can be dealt.
func TestForceDrawWashesHand(t *testing.T) {
	g := newTestGame()
	g.Players[0].Score = 12
	g.Players[1].Score = 5
	g.Players[0].Hand = parse("2D")
	g.Players[1].Hand = parse("9C")
	g.Phase = PhasePlay

	if _, err := g.Apply(1, gr.Action{Type: "forceDraw"}); err != nil {
		t.Fatalf("forceDraw: %v", err)
	}
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("phase=%s, want roundEnd", g.Phase)
	}
	if g.Players[0].Score != 12 || g.Players[1].Score != 5 {
		t.Fatalf("scores changed on a wash: %d/%d", g.Players[0].Score, g.Players[1].Score)
	}
	if _, err := g.Apply(1, gr.Action{Type: "forceDraw"}); err == nil {
		t.Fatal("expected forceDraw to be rejected after the hand ended")
	}
}

// TestRemovePlayerRedealsOrCancels confirms declining an invite re-deals to the
// remaining players and signals cancellation when fewer than two are left.
func TestRemovePlayerRedealsOrCancels(t *testing.T) {
	players := []*gr.Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}, {UserID: 3, Username: "C"}}
	g := NewGame(1, players, 100)
	remaining, removed := g.RemovePlayer(2)
	if !removed || remaining != 2 {
		t.Fatalf("remove: removed=%v remaining=%d", removed, remaining)
	}
	for _, p := range g.Players {
		if p.UserID == 2 {
			t.Fatal("declined player should be gone")
		}
		if len(p.Hand) != g.HandSize {
			t.Fatalf("re-deal should give %d cards, got %d", g.HandSize, len(p.Hand))
		}
	}
	if g.Phase != PhaseDraw {
		t.Fatalf("phase=%s, want draw", g.Phase)
	}
	if remaining, removed = g.RemovePlayer(1); !removed || remaining != 1 {
		t.Fatalf("dropping below 2 should report remaining=1 removed=true, got %d %v", remaining, removed)
	}
}

func TestDealGivesSevenEach(t *testing.T) {
	for n := 2; n <= 4; n++ {
		players := make([]*gr.Player, n)
		for i := range players {
			players[i] = &gr.Player{UserID: i + 1, Username: "p"}
		}
		g := NewGame(1, players, 100)
		for i, p := range g.Players {
			if len(p.Hand) != 7 {
				t.Fatalf("%d players: seat %d got %d cards, want 7", n, i, len(p.Hand))
			}
		}
		want := 52 - 7*n - 1
		if len(g.Stock) != want {
			t.Fatalf("%d players: stock=%d, want %d", n, len(g.Stock), want)
		}
		if len(g.DiscardPile) != 1 {
			t.Fatalf("%d players: discard=%d, want 1", n, len(g.DiscardPile))
		}
	}
}

func TestMeldLayoffAndGoOut(t *testing.T) {
	g := newTestGame()
	g.Players[0].Hand = parse("4H", "5H", "6H", "2C")
	g.Players[1].Hand = parse("7S", "9C", "KD") // 7+9+10 = 26
	g.Table = []*TableMeld{tableMeld("3C", "4C", "5C")}

	if _, wentOut, err := g.Meld(1, []string{"4H", "5H", "6H"}); err != nil || wentOut {
		t.Fatalf("Meld run: wentOut=%v err=%v", wentOut, err)
	}
	if len(g.Table) != 2 {
		t.Fatalf("table has %d melds, want 2", len(g.Table))
	}
	if len(g.Players[0].Hand) != 1 {
		t.Fatalf("hand after meld = %v, want [2C]", gr.Codes(g.Players[0].Hand))
	}

	_, wentOut, err := g.Layoff(1, "2C", 0)
	if err != nil {
		t.Fatalf("Layoff 2C onto clubs run: %v", err)
	}
	if !wentOut {
		t.Fatal("expected player A to go out after laying off last card")
	}
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("phase=%s, want roundEnd", g.Phase)
	}
	if g.Players[0].Score != 26 {
		t.Fatalf("winner score=%d, want 26", g.Players[0].Score)
	}
	if g.Table[0].Codes[0] != "2C" {
		t.Fatalf("clubs run did not grow: %v", g.Table[0].Codes)
	}
}

func TestDiscardGoOut(t *testing.T) {
	g := newTestGame()
	g.Players[0].Hand = parse("7C", "7D", "7S", "KH") // set of 7s + one deadwood
	g.Players[1].Hand = parse("2C", "3C")             // 2+3 = 5

	if _, wentOut, err := g.Meld(1, []string{"7C", "7D", "7S"}); err != nil || wentOut {
		t.Fatalf("Meld set: wentOut=%v err=%v", wentOut, err)
	}
	card, wentOut, err := g.Discard(1, "KH")
	if err != nil || !wentOut {
		t.Fatalf("Discard last card: card=%s wentOut=%v err=%v", card.Code(), wentOut, err)
	}
	if g.Players[0].Score != 5 {
		t.Fatalf("winner score=%d, want 5", g.Players[0].Score)
	}
}

func TestInvalidMeldRejected(t *testing.T) {
	g := newTestGame()
	g.Players[0].Hand = parse("4H", "5H", "7H", "2C")
	if _, _, err := g.Meld(1, []string{"4H", "5H", "7H"}); err == nil {
		t.Fatal("expected invalid meld (non-consecutive run) to be rejected")
	}
	if len(g.Table) != 0 {
		t.Fatal("invalid meld should not reach the table")
	}
}

func TestScoreAccumulatesAcrossHands(t *testing.T) {
	g := newTestGame()
	g.Players[0].Score = 26
	g.Players[1].Score = 14
	g.Phase = PhaseRoundEnd
	if err := g.NextHand(); err != nil {
		t.Fatalf("NextHand: %v", err)
	}
	if g.Players[0].Score != 26 || g.Players[1].Score != 14 {
		t.Fatalf("scores reset on new hand: %d / %d, want 26 / 14",
			g.Players[0].Score, g.Players[1].Score)
	}
	for i, p := range g.Players {
		if len(p.Hand) != 7 {
			t.Fatalf("seat %d dealt %d cards, want 7", i, len(p.Hand))
		}
	}
}

func TestScoreSumsOverMultipleHands(t *testing.T) {
	g := newTestGame()
	// Hand 1: A goes out, B holds KD,KS (20).
	g.Players[0].Hand = nil
	g.Players[1].Hand = parse("KD", "KS")
	g.goOut(0)
	if g.Players[0].Score != 20 {
		t.Fatalf("after hand 1 score=%d, want 20", g.Players[0].Score)
	}
	g.Phase = PhaseRoundEnd
	if err := g.NextHand(); err != nil {
		t.Fatalf("NextHand: %v", err)
	}
	// Hand 2: A goes out again, B holds 5C,6C (11). Score must SUM to 31.
	g.Players[0].Hand = nil
	g.Players[1].Hand = parse("5C", "6C")
	g.Turn = 0
	g.goOut(0)
	if g.Players[0].Score != 31 {
		t.Fatalf("after hand 2 cumulative score=%d, want 31 (20+11)", g.Players[0].Score)
	}
}

func TestBlockedHandEndsAndScoresLowest(t *testing.T) {
	g := newTestGame()
	// A holds the Ace of Clubs (1); B holds the Queen of Spades (10). Neither can
	// be melded or laid off — a deadlock once the deck has cycled with no progress.
	g.Players[0].Hand = parse("AC")
	g.Players[1].Hand = parse("QS")
	g.Stock = nil
	g.DiscardPile = parse("8D", "8S") // >1 card so a reshuffle is otherwise possible
	g.reshuffles = 1                  // already cycled once
	g.progressed = false              // ...with no meld/lay-off since
	g.Phase = PhaseDraw
	g.Turn = 0

	_, err := g.Draw(1, false) // A tries to draw from the (empty) stock
	if err != errHandEnded {
		t.Fatalf("expected errHandEnded on a blocked draw, got %v", err)
	}
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("phase=%s, want roundEnd (hand should end, not loop)", g.Phase)
	}
	if g.Players[0].Score != 10 {
		t.Fatalf("lowest-count winner score=%d, want 10 (B's Queen)", g.Players[0].Score)
	}
	if !g.LastResults[0].Blocked {
		t.Fatal("winner result should be flagged Blocked")
	}
}

func TestReshuffleAllowedWhenProgressing(t *testing.T) {
	g := newTestGame()
	g.Players[0].Hand = parse("AC")
	g.Players[1].Hand = parse("QS")
	g.Stock = nil
	g.DiscardPile = parse("8D", "8S", "9S")
	g.reshuffles = 1
	g.progressed = true // progress happened since the last reshuffle → keep playing
	g.Phase = PhaseDraw
	g.Turn = 0
	if _, err := g.Draw(1, false); err != nil {
		t.Fatalf("draw after progress should reshuffle, got %v", err)
	}
	if g.Phase != PhasePlay {
		t.Fatalf("phase=%s, want play (stock refilled)", g.Phase)
	}
	if g.reshuffles != 2 || g.progressed {
		t.Fatalf("reshuffle bookkeeping wrong: reshuffles=%d progressed=%v", g.reshuffles, g.progressed)
	}
}

func TestDiscardEndsTurnAndRequiresDraw(t *testing.T) {
	g := newTestGame()
	g.Players[0].Hand = parse("4H", "5H", "9C", "KD")
	g.Players[1].Hand = parse("2C", "3C", "8S")

	if _, _, err := g.Discard(1, "KD"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if g.Turn != 1 || g.Phase != PhaseDraw {
		t.Fatalf("after discard turn=%d phase=%s, want 1/draw", g.Turn, g.Phase)
	}
	// Player B must draw before acting.
	if _, _, err := g.Discard(2, "8S"); err == nil {
		t.Fatal("expected error discarding before drawing")
	}
}
