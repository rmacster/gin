package ginrummy

import "encoding/json"

// The snapshot types capture the full game state for persistence so a game can
// survive a server restart. Card is an int, so it serializes as a plain number.

type meldSnap struct {
	Kind  string `json:"kind"`
	Cards []Card `json:"cards"`
}

type resultSnap struct {
	UserID    int        `json:"user_id"`
	Username  string     `json:"username"`
	Melds     []meldSnap `json:"melds"`
	Deadwood  int        `json:"deadwood"`
	Points    int        `json:"points"`
	IsKnocker bool       `json:"is_knocker"`
	Gin       bool       `json:"gin"`
	Undercut  bool       `json:"undercut"`
	HandCodes []string   `json:"hand"`
}

type playerSnap struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	IsRobot  bool   `json:"is_robot"`
	Hand     []Card `json:"hand"`
	Score    int    `json:"score"`
}

type gameSnap struct {
	ID           int          `json:"id"`
	Players      []playerSnap `json:"players"`
	Stock        []Card       `json:"stock"`
	Discard      []Card       `json:"discard"`
	Turn         int          `json:"turn"`
	Phase        string       `json:"phase"`
	HandNumber   int          `json:"hand_number"`
	DealerIdx    int          `json:"dealer_idx"`
	TargetScore  int          `json:"target_score"`
	HandSize     int          `json:"hand_size"`
	WinnerID     int          `json:"winner_id"`
	LastDrawFrom string       `json:"last_draw_from"`
	LastResults  []resultSnap `json:"last_results"`
}

// Snapshot serializes the full game state. Caller holds the lock.
func (g *Game) Snapshot() ([]byte, error) {
	s := gameSnap{
		ID: g.ID, Stock: g.Stock, Discard: g.DiscardPile, Turn: g.Turn, Phase: g.Phase,
		HandNumber: g.HandNumber, DealerIdx: g.DealerIdx, TargetScore: g.TargetScore,
		HandSize: g.HandSize, WinnerID: g.WinnerID, LastDrawFrom: g.LastDrawFrom,
	}
	for _, p := range g.Players {
		s.Players = append(s.Players, playerSnap{p.UserID, p.Username, p.IsRobot, p.Hand, p.Score})
	}
	for _, r := range g.LastResults {
		rs := resultSnap{
			UserID: r.UserID, Username: r.Username, Deadwood: r.Deadwood, Points: r.Points,
			IsKnocker: r.IsKnocker, Gin: r.Gin, Undercut: r.Undercut, HandCodes: r.HandCodes,
		}
		for _, m := range r.Melds {
			rs.Melds = append(rs.Melds, meldSnap{m.Kind, m.Cards})
		}
		s.LastResults = append(s.LastResults, rs)
	}
	return json.Marshal(s)
}

// LoadGame reconstructs a Game from a snapshot produced by Snapshot.
func LoadGame(data []byte) (*Game, error) {
	var s gameSnap
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	g := &Game{
		ID: s.ID, Stock: s.Stock, DiscardPile: s.Discard, Turn: s.Turn, Phase: s.Phase,
		HandNumber: s.HandNumber, DealerIdx: s.DealerIdx, TargetScore: s.TargetScore,
		HandSize: s.HandSize, WinnerID: s.WinnerID, LastDrawFrom: s.LastDrawFrom,
	}
	for _, p := range s.Players {
		g.Players = append(g.Players, &Player{
			UserID: p.UserID, Username: p.Username, IsRobot: p.IsRobot, Hand: p.Hand, Score: p.Score,
		})
	}
	for _, r := range s.LastResults {
		hr := HandResult{
			UserID: r.UserID, Username: r.Username, Deadwood: r.Deadwood, Points: r.Points,
			IsKnocker: r.IsKnocker, Gin: r.Gin, Undercut: r.Undercut, HandCodes: r.HandCodes,
		}
		for _, m := range r.Melds {
			hr.Melds = append(hr.Melds, Meld{Kind: m.Kind, Cards: m.Cards, Codes: codes(m.Cards)})
		}
		g.LastResults = append(g.LastResults, hr)
	}
	return g, nil
}
