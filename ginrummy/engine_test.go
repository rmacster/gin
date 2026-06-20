package ginrummy

import "testing"

// helper: build a card from rank(0..12)+suit(0..3)
func mk(rank, suit int) Card { return Card(rank*4 + suit) }

func TestValue(t *testing.T) {
	cases := map[Card]int{mk(0, 0): 1, mk(1, 0): 2, mk(9, 0): 10, mk(10, 0): 10, mk(12, 3): 10}
	for c, want := range cases {
		if c.Value() != want {
			t.Errorf("%s value = %d, want %d", c.Code(), c.Value(), want)
		}
	}
}

func TestRunMeld(t *testing.T) {
	// 5,6,7 of clubs (suit 0) + three random deadwood
	hand := []Card{mk(4, 0), mk(5, 0), mk(6, 0), mk(0, 1), mk(2, 2), mk(8, 3)}
	a := Analyze(hand)
	if len(a.Melds) != 1 || a.Melds[0].Kind != "run" {
		t.Fatalf("expected one run meld, got %+v", a.Melds)
	}
	// deadwood = A(1) + 3(3) + 9(9) = 13
	if a.Deadwood != 13 {
		t.Errorf("deadwood = %d, want 13", a.Deadwood)
	}
}

func TestSetMeld(t *testing.T) {
	// three Kings + low cards
	hand := []Card{mk(12, 0), mk(12, 1), mk(12, 2), mk(0, 0), mk(1, 1)}
	a := Analyze(hand)
	if len(a.Melds) != 1 || a.Melds[0].Kind != "set" {
		t.Fatalf("expected one set meld, got %+v", a.Melds)
	}
	if a.Deadwood != 3 { // A + 2
		t.Errorf("deadwood = %d, want 3", a.Deadwood)
	}
}

func TestGinHand(t *testing.T) {
	// 10-card gin: run 4-5-6-7 clubs, set of 9s, run 2-3-4 hearts? build clean
	// Sets: 9C,9D,9H. Run: AC,2C,3C,4C. Run: 5D,6D,7D
	hand := []Card{
		mk(8, 0), mk(8, 1), mk(8, 2), // 9 9 9
		mk(0, 0), mk(1, 0), mk(2, 0), mk(3, 0), // A 2 3 4 clubs
		mk(4, 1), mk(5, 1), mk(6, 1), // 5 6 7 diamonds
	}
	a := Analyze(hand)
	if a.Deadwood != 0 {
		t.Errorf("expected gin (0 deadwood), got %d melds=%+v", a.Deadwood, a.Melds)
	}
}

func TestLayOff(t *testing.T) {
	// knocker meld: 5,6,7 clubs. opponent deadwood includes 8 clubs (extends run) and 4 clubs.
	melds := []Meld{{Kind: "run", Cards: []Card{mk(4, 0), mk(5, 0), mk(6, 0)}}}
	dead := []Card{mk(7, 0), mk(3, 0), mk(11, 3)} // 8C extends high, 4C extends low, QS stays
	rem, laid := LayOff(dead, melds)
	if len(laid) != 2 {
		t.Errorf("expected 2 laid off, got %d (%v)", len(laid), codes(laid))
	}
	if rem != 10 { // only QS remains = 10
		t.Errorf("remaining deadwood = %d, want 10", rem)
	}
}

func TestGameFlow(t *testing.T) {
	players := []*Player{
		{UserID: 1, Username: "A"},
		{UserID: 2, Username: "B"},
	}
	g := NewGame(1, players, 100)
	if g.HandSize != 10 {
		t.Fatalf("2-player hand size = %d, want 10", g.HandSize)
	}
	if len(g.Players[0].Hand) != 10 || len(g.Players[1].Hand) != 10 {
		t.Fatalf("hands not dealt correctly")
	}
	if len(g.Stock) != 31 {
		t.Fatalf("stock = %d, want 31", len(g.Stock))
	}
	// Player 2 starts (left of dealer 0).
	if g.CurrentPlayer().UserID != 2 {
		t.Fatalf("expected player 2 to start, got %d", g.CurrentPlayer().UserID)
	}
	// Draw from stock then discard the first card.
	if _, err := g.Draw(2, false); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if g.Phase != PhaseDiscard {
		t.Fatalf("phase = %s, want discard", g.Phase)
	}
	d := g.Players[1].Hand[0]
	if err := g.Discard(2, d, false); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if g.CurrentPlayer().UserID != 1 {
		t.Fatalf("turn did not advance to player 1")
	}
}

func TestThreeHandedDeal(t *testing.T) {
	players := []*Player{{UserID: 1, Username: "A"}, {UserID: 2, Username: "B"}, {UserID: 3, Username: "C"}}
	g := NewGame(1, players, 100)
	if g.HandSize != 7 {
		t.Fatalf("3-player hand size = %d, want 7", g.HandSize)
	}
	if len(g.Stock) != 52-21-1 {
		t.Fatalf("stock = %d, want %d", len(g.Stock), 52-21-1)
	}
}
