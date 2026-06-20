package rummy

import gr "gin-server/ginrummy"

// RobotStep plays one logical robot action (draw, one meld, one lay-off, or the
// final discard). The hub calls it repeatedly with pauses between, so robots
// visibly lay melds one at a time. Caller holds the lock.
func (g *RummyGame) RobotStep() (actorID int, actorName string, verbs []string, acted bool) {
	if g.Phase != PhaseDraw && g.Phase != PhasePlay {
		return 0, "", nil, false
	}
	p := g.Players[g.Turn]
	if !p.IsRobot {
		return 0, "", nil, false
	}
	idx := g.Turn
	actorID, actorName = p.UserID, p.Username

	if g.Phase == PhaseDraw {
		fromDiscard := g.robotWantsDiscard(idx)
		c, err := g.Draw(actorID, fromDiscard)
		if err != nil {
			// The deck cycled with no possible play — the hand is blocked.
			return actorID, actorName, []string{"is stuck — the deck cycled with no plays left, so the hand is blocked"}, true
		}
		if fromDiscard {
			return actorID, actorName, []string{"drew the " + c.Name() + " from the discard"}, true
		}
		return actorID, actorName, []string{"drew from the stock"}, true
	}

	// Play phase: lay a meld, then lay off, then discard — one action per call.
	if meld, ok := g.robotNextMeld(idx); ok {
		cards, wentOut, err := g.Meld(actorID, gr.Codes(meld))
		if err == nil {
			return actorID, actorName, outVerbs("laid down "+describeMeld(cards), wentOut), true
		}
	}
	if code, mi, ok := g.robotNextLayoff(idx); ok {
		card, wentOut, err := g.Layoff(actorID, code, mi)
		if err == nil {
			return actorID, actorName, outVerbs("laid off the "+card.Name(), wentOut), true
		}
	}
	code := g.robotDiscardChoice(idx)
	card, wentOut, err := g.Discard(actorID, code)
	if err != nil {
		return actorID, actorName, nil, false
	}
	return actorID, actorName, outVerbs("discarded the "+card.Name(), wentOut), true
}

// robotWantsDiscard reports whether taking the discard top helps the robot —
// either it lays off onto a table meld or it becomes part of a meld in hand.
func (g *RummyGame) robotWantsDiscard(idx int) bool {
	top, ok := g.DiscardTop()
	if !ok {
		return false
	}
	for _, tm := range g.Table {
		if _, can := gr.CanLayOff(tm.asMeld(), top); can {
			return true
		}
	}
	withCard := append(append([]gr.Card{}, g.Players[idx].Hand...), top)
	for _, m := range gr.Analyze(withCard).Melds {
		for _, c := range m.Cards {
			if c == top {
				return true
			}
		}
	}
	return false
}

// robotNextMeld returns a meld the robot can lay down now, if any.
func (g *RummyGame) robotNextMeld(idx int) ([]gr.Card, bool) {
	for _, m := range gr.Analyze(g.Players[idx].Hand).Melds {
		if len(m.Cards) >= 3 {
			return m.Cards, true
		}
	}
	return nil, false
}

// robotNextLayoff returns a deadwood card and target meld the robot can lay off.
func (g *RummyGame) robotNextLayoff(idx int) (code string, meldIdx int, ok bool) {
	for _, c := range gr.Analyze(g.Players[idx].Hand).Unmatched {
		for mi, tm := range g.Table {
			if _, can := gr.CanLayOff(tm.asMeld(), c); can {
				return c.Code(), mi, true
			}
		}
	}
	return "", 0, false
}

// robotDiscardChoice picks the highest-value unmatched card to discard.
func (g *RummyGame) robotDiscardChoice(idx int) string {
	hand := g.Players[idx].Hand
	unmatched := gr.Analyze(hand).Unmatched
	pool := unmatched
	if len(pool) == 0 {
		pool = hand
	}
	best := pool[0]
	for _, c := range pool[1:] {
		if c.Value() > best.Value() {
			best = c
		}
	}
	return best.Code()
}
