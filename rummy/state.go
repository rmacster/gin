package rummy

import gr "gin-server/ginrummy"

type playerView struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	IsRobot   bool   `json:"is_robot"`
	Score     int    `json:"score"`
	HandCount int    `json:"hand_count"`
	Connected bool   `json:"connected"`
	IsTurn    bool   `json:"is_turn"`
	IsDealer  bool   `json:"is_dealer"`
}

// StateFor builds the view of the game for a specific user.
func (g *RummyGame) StateFor(userID int) map[string]interface{} {
	playing := g.Phase == PhaseDraw || g.Phase == PhasePlay

	players := make([]playerView, 0, len(g.Players))
	for i, p := range g.Players {
		players = append(players, playerView{
			UserID:    p.UserID,
			Username:  p.Username,
			IsRobot:   p.IsRobot,
			Score:     p.Score,
			HandCount: len(p.Hand),
			Connected: p.Connected,
			IsTurn:    playing && i == g.Turn,
			IsDealer:  i == g.DealerIdx,
		})
	}

	turnUser := 0
	if playing {
		turnUser = g.Players[g.Turn].UserID
	}

	var dtop interface{}
	if c, ok := g.DiscardTop(); ok {
		dtop = c.Code()
	}

	table := make([]map[string]interface{}, 0, len(g.Table))
	for _, tm := range g.Table {
		table = append(table, map[string]interface{}{
			"kind":  tm.Kind,
			"cards": tm.Codes,
			"owner": tm.Owner,
		})
	}

	state := map[string]interface{}{
		"game_id":      g.ID,
		"game_type":    "rummy",
		"phase":        g.Phase,
		"hand_number":  g.HandNumber,
		"target_score": g.TargetScore,
		"hand_size":    g.HandSize,
		"turn_user_id": turnUser,
		"winner_id":    g.WinnerID,
		"stock_count":  len(g.Stock),
		"discard_top":  dtop,
		"players":      players,
		"table":        table,
	}

	if idx := g.playerIndex(userID); idx >= 0 {
		p := g.Players[idx]
		a := gr.Analyze(p.Hand)
		rem := 0
		for _, c := range p.Hand {
			rem += c.Value()
		}
		state["your_hand"] = gr.Codes(p.Hand)
		state["your_analysis"] = map[string]interface{}{
			"melds":     a.Melds, // suggested melds the player could lay down
			"remaining": rem,
		}
	}

	if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
		state["results"] = g.LastResults
	}

	return state
}
