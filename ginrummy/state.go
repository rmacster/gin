package ginrummy

// PlayerView is the public info about a seat (no hand contents).
type PlayerView struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	IsRobot   bool   `json:"is_robot"`
	Score     int    `json:"score"`
	HandCount int    `json:"hand_count"`
	Connected bool   `json:"connected"`
	IsTurn    bool   `json:"is_turn"`
	IsDealer  bool   `json:"is_dealer"`
}

// StateFor builds the view of the game for a specific user. If userID is 0 or
// not a player (e.g. an observer), no private hand is included.
func (g *Game) StateFor(userID int) map[string]interface{} {
	var players []PlayerView
	for i, p := range g.Players {
		players = append(players, PlayerView{
			UserID:    p.UserID,
			Username:  p.Username,
			IsRobot:   p.IsRobot,
			Score:     p.Score,
			HandCount: len(p.Hand),
			Connected: p.Connected,
			IsTurn:    (g.Phase == PhaseDraw || g.Phase == PhaseDiscard) && i == g.Turn,
			IsDealer:  i == g.DealerIdx,
		})
	}

	turnUser := 0
	if cp := g.CurrentPlayer(); cp != nil {
		turnUser = cp.UserID
	}

	dtop := interface{}(nil)
	if c, ok := g.DiscardTop(); ok {
		dtop = c.Code()
	}

	state := map[string]interface{}{
		"game_id":      g.ID,
		"game_type":    "gin",
		"phase":        g.Phase,
		"hand_number":  g.HandNumber,
		"target_score": g.TargetScore,
		"hand_size":    g.HandSize,
		"turn_user_id": turnUser,
		"winner_id":    g.WinnerID,
		"stock_count":  len(g.Stock),
		"discard_top":  dtop,
		"players":      players,
	}

	if idx := g.playerIndex(userID); idx >= 0 {
		p := g.Players[idx]
		a := Analyze(p.Hand)
		state["your_hand"] = codes(p.Hand)
		canKnock := false
		// You may knock if, after discarding your worst-case card, deadwood <= 10.
		// During the discard phase your hand is HandSize+1; we report whether a
		// knock is reachable at all (best deadwood after the optimal discard).
		if g.Phase == PhaseDiscard && idx == g.Turn {
			canKnock = bestKnockDeadwood(p.Hand) <= KnockThreshold
		}
		state["your_analysis"] = map[string]interface{}{
			"deadwood":  a.Deadwood,
			"melds":     a.Melds,
			"can_knock": canKnock,
		}
	}

	if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
		state["results"] = g.LastResults
	}

	return state
}

// bestKnockDeadwood returns the minimum deadwood achievable after discarding one
// card from an (oversized) hand.
func bestKnockDeadwood(hand []Card) int {
	best := 1 << 30
	for i := range hand {
		rest := append([]Card{}, hand[:i]...)
		rest = append(rest, hand[i+1:]...)
		if d := Analyze(rest).Deadwood; d < best {
			best = d
		}
	}
	return best
}
