package rummy

import (
	"encoding/json"

	gr "gin-server/ginrummy"
)

type playerSnap struct {
	UserID   int       `json:"user_id"`
	Username string    `json:"username"`
	IsRobot  bool      `json:"is_robot"`
	Hand     []gr.Card `json:"hand"`
	Score    int       `json:"score"`
}

type tableSnap struct {
	Kind  string    `json:"kind"`
	Cards []gr.Card `json:"cards"`
	Owner int       `json:"owner"`
}

type resultSnap struct {
	UserID    int      `json:"user_id"`
	Username  string   `json:"username"`
	WentOut   bool     `json:"went_out"`
	Blocked   bool     `json:"blocked"`
	Remaining int      `json:"remaining"`
	Points    int      `json:"points"`
	HandCodes []string `json:"hand"`
}

type gameSnap struct {
	ID          int          `json:"id"`
	Players     []playerSnap `json:"players"`
	Stock       []gr.Card    `json:"stock"`
	Discard     []gr.Card    `json:"discard"`
	Table       []tableSnap  `json:"table"`
	Turn        int          `json:"turn"`
	Phase       string       `json:"phase"`
	HandNumber  int          `json:"hand_number"`
	DealerIdx   int          `json:"dealer_idx"`
	TargetScore int          `json:"target_score"`
	HandSize    int          `json:"hand_size"`
	WinnerID    int          `json:"winner_id"`
	Progressed  bool         `json:"progressed"`
	Reshuffles  int          `json:"reshuffles"`
	LastResults []resultSnap `json:"last_results"`
}

// Snapshot serializes the full game state. Caller holds the lock.
func (g *RummyGame) Snapshot() ([]byte, error) {
	s := gameSnap{
		ID: g.ID, Stock: g.Stock, Discard: g.DiscardPile, Turn: g.Turn, Phase: g.Phase,
		HandNumber: g.HandNumber, DealerIdx: g.DealerIdx, TargetScore: g.TargetScore,
		HandSize: g.HandSize, WinnerID: g.WinnerID, Progressed: g.progressed, Reshuffles: g.reshuffles,
	}
	for _, p := range g.Players {
		s.Players = append(s.Players, playerSnap{p.UserID, p.Username, p.IsRobot, p.Hand, p.Score})
	}
	for _, tm := range g.Table {
		s.Table = append(s.Table, tableSnap{tm.Kind, tm.Cards, tm.Owner})
	}
	for _, r := range g.LastResults {
		s.LastResults = append(s.LastResults, resultSnap{
			UserID: r.UserID, Username: r.Username, WentOut: r.WentOut, Blocked: r.Blocked,
			Remaining: r.Remaining, Points: r.Points, HandCodes: r.HandCodes,
		})
	}
	return json.Marshal(s)
}

// LoadGame reconstructs a RummyGame from a snapshot produced by Snapshot.
func LoadGame(data []byte) (*RummyGame, error) {
	var s gameSnap
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	g := &RummyGame{
		ID: s.ID, Stock: s.Stock, DiscardPile: s.Discard, Turn: s.Turn, Phase: s.Phase,
		HandNumber: s.HandNumber, DealerIdx: s.DealerIdx, TargetScore: s.TargetScore,
		HandSize: s.HandSize, WinnerID: s.WinnerID, progressed: s.Progressed, reshuffles: s.Reshuffles,
	}
	for _, p := range s.Players {
		g.Players = append(g.Players, &gr.Player{
			UserID: p.UserID, Username: p.Username, IsRobot: p.IsRobot, Hand: p.Hand, Score: p.Score,
		})
	}
	for _, tm := range s.Table {
		g.Table = append(g.Table, &TableMeld{Kind: tm.Kind, Cards: tm.Cards, Codes: gr.Codes(tm.Cards), Owner: tm.Owner})
	}
	for _, r := range s.LastResults {
		g.LastResults = append(g.LastResults, HandResult{
			UserID: r.UserID, Username: r.Username, WentOut: r.WentOut, Blocked: r.Blocked,
			Remaining: r.Remaining, Points: r.Points, HandCodes: r.HandCodes,
		})
	}
	return g, nil
}
