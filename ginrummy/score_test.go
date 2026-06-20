package ginrummy

import "testing"

// TestGinStockExhaustionEndsHand confirms Gin cannot deadlock: when the stock is
// exhausted (Gin never reshuffles the discard), the hand washes and ends.
func TestGinStockExhaustionEndsHand(t *testing.T) {
	players := []*Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}}
	g := NewGame(1, players, 100)
	g.Stock = nil // simulate an exhausted stock
	g.Phase = PhaseDraw

	turn := g.TurnUserID()
	_, err := g.Draw(turn, false) // draw from the empty stock
	if err != ErrEmptyStock {
		t.Fatalf("expected ErrEmptyStock, got %v", err)
	}
	if g.Phase != PhaseRoundEnd && g.Phase != PhaseGameOver {
		t.Fatalf("phase=%s, want the hand to have ended (roundEnd/gameOver)", g.Phase)
	}
}

// TestGinForceDrawWashesHand confirms a consensual draw ends the hand with no
// score change and lands in roundEnd so the next hand can be dealt.
func TestGinForceDrawWashesHand(t *testing.T) {
	players := []*Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}}
	g := NewGame(1, players, 100)
	g.Players[0].Score = 7
	g.Players[1].Score = 3
	g.Phase = PhaseDraw

	if _, err := g.Apply(1, Action{Type: "forceDraw"}); err != nil {
		t.Fatalf("forceDraw: %v", err)
	}
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("phase=%s, want roundEnd", g.Phase)
	}
	if g.Players[0].Score != 7 || g.Players[1].Score != 3 {
		t.Fatalf("scores changed on a wash: %d/%d", g.Players[0].Score, g.Players[1].Score)
	}
	// A washed hand must be over: forceDraw again should be rejected.
	if _, err := g.Apply(1, Action{Type: "forceDraw"}); err == nil {
		t.Fatal("expected forceDraw to be rejected after the hand ended")
	}
}

// TestGinRemovePlayerRedealsOrCancels confirms declining an invite re-deals to
// the remaining players (switching hand size) and signals cancellation below 2.
func TestGinRemovePlayerRedealsOrCancels(t *testing.T) {
	players := []*Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}, {UserID: 3, Username: "C"}}
	g := NewGame(1, players, 100)
	if g.HandSize != 7 {
		t.Fatalf("3 players should deal 7, got %d", g.HandSize)
	}
	remaining, removed := g.RemovePlayer(2)
	if !removed || remaining != 2 {
		t.Fatalf("remove: removed=%v remaining=%d", removed, remaining)
	}
	if g.HandSize != 10 {
		t.Fatalf("dropping to 2 players should switch to a 10-card deal, got %d", g.HandSize)
	}
	for _, p := range g.Players {
		if p.UserID == 2 {
			t.Fatal("declined player should be gone")
		}
		if len(p.Hand) != 10 {
			t.Fatalf("re-deal should give 10 cards, got %d", len(p.Hand))
		}
	}
	if g.Phase != PhaseDraw || g.Turn < 0 || g.Turn >= len(g.Players) {
		t.Fatalf("bad post-redeal state: phase=%s turn=%d", g.Phase, g.Turn)
	}
	if remaining, removed = g.RemovePlayer(1); !removed || remaining != 1 {
		t.Fatalf("dropping below 2 should report remaining=1 removed=true, got %d %v", remaining, removed)
	}
}

// TestGinScoreAccumulatesAcrossHands confirms a new hand keeps the running score.
func TestGinScoreAccumulatesAcrossHands(t *testing.T) {
	players := []*Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}}
	g := NewGame(1, players, 100)
	g.Players[0].Score = 33
	g.Players[1].Score = 21
	g.Phase = PhaseRoundEnd

	if err := g.NextHand(); err != nil {
		t.Fatalf("NextHand: %v", err)
	}
	if g.Players[0].Score != 33 || g.Players[1].Score != 21 {
		t.Fatalf("scores reset on new hand: %d / %d, want 33 / 21",
			g.Players[0].Score, g.Players[1].Score)
	}
	if len(g.Players[0].Hand) != 10 {
		t.Fatalf("new hand dealt %d cards, want 10", len(g.Players[0].Hand))
	}
}
