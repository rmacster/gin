package ginrummy

import "sort"

// Meld is a set (3-4 same rank) or run (3+ consecutive, same suit) of cards.
type Meld struct {
	Kind  string `json:"kind"` // "set" or "run"
	Cards []Card `json:"-"`
	Codes []string `json:"cards"`
}

// Analysis is the best (minimal-deadwood) arrangement of a hand.
type Analysis struct {
	Deadwood int
	Melds    []Meld
	Unmatched []Card // leftover deadwood cards
}

// generateMelds returns every candidate meld in the hand as a bitmask over the
// hand slice indices, paired with the meld kind.
func generateMelds(hand []Card) (masks []uint32, kinds []string) {
	n := len(hand)

	// Sets: group indices by rank.
	byRank := map[int][]int{}
	for i, c := range hand {
		byRank[c.Rank()] = append(byRank[c.Rank()], i)
	}
	for _, idxs := range byRank {
		if len(idxs) >= 3 {
			// All 3-subsets and the full 4-set.
			for _, combo := range combinations(idxs, 3) {
				masks = append(masks, maskOf(combo))
				kinds = append(kinds, "set")
			}
			if len(idxs) == 4 {
				masks = append(masks, maskOf(idxs))
				kinds = append(kinds, "set")
			}
		}
	}

	// Runs: group indices by suit, sort by rank, find contiguous stretches.
	bySuit := map[int][]int{}
	for i := range hand {
		bySuit[hand[i].Suit()] = append(bySuit[hand[i].Suit()], i)
	}
	for _, idxs := range bySuit {
		sort.Slice(idxs, func(a, b int) bool { return hand[idxs[a]].Rank() < hand[idxs[b]].Rank() })
		// Walk contiguous runs of consecutive ranks (no duplicate ranks within a suit).
		start := 0
		for start < len(idxs) {
			end := start
			for end+1 < len(idxs) && hand[idxs[end+1]].Rank() == hand[idxs[end]].Rank()+1 {
				end++
			}
			run := idxs[start : end+1]
			// Every contiguous sub-run of length >= 3.
			for i := 0; i < len(run); i++ {
				for j := i + 2; j < len(run); j++ {
					masks = append(masks, maskOf(run[i:j+1]))
					kinds = append(kinds, "run")
				}
			}
			start = end + 1
		}
	}
	_ = n
	return
}

func maskOf(idxs []int) uint32 {
	var m uint32
	for _, i := range idxs {
		m |= 1 << uint(i)
	}
	return m
}

func combinations(items []int, k int) [][]int {
	var out [][]int
	var rec func(start int, cur []int)
	rec = func(start int, cur []int) {
		if len(cur) == k {
			cp := make([]int, k)
			copy(cp, cur)
			out = append(out, cp)
			return
		}
		for i := start; i < len(items); i++ {
			rec(i+1, append(cur, items[i]))
		}
	}
	rec(0, nil)
	return out
}

// Analyze finds the arrangement of melds minimizing total deadwood.
func Analyze(hand []Card) Analysis {
	masks, kinds := generateMelds(hand)

	full := uint32(0)
	for i := range hand {
		full |= 1 << uint(i)
	}

	type res struct {
		dw    int
		picks []int // indices into masks
	}
	memo := map[uint32]res{}

	var solve func(avail uint32) res
	solve = func(avail uint32) res {
		if avail == 0 {
			return res{0, nil}
		}
		if r, ok := memo[avail]; ok {
			return r
		}
		// Lowest available card index.
		low := 0
		for (avail>>uint(low))&1 == 0 {
			low++
		}
		// Option A: leave `low` as deadwood.
		best := solve(avail &^ (1 << uint(low)))
		best = res{best.dw + hand[low].Value(), best.picks}
		// Option B: use a meld containing `low` fully inside avail.
		for mi, m := range masks {
			if m&(1<<uint(low)) == 0 {
				continue
			}
			if m&avail != m {
				continue
			}
			sub := solve(avail &^ m)
			if sub.dw < best.dw {
				picks := append([]int{mi}, sub.picks...)
				best = res{sub.dw, picks}
			}
		}
		memo[avail] = best
		return best
	}

	r := solve(full)

	used := uint32(0)
	var melds []Meld
	for _, mi := range r.picks {
		m := masks[mi]
		used |= m
		var cs []Card
		for i := range hand {
			if m&(1<<uint(i)) != 0 {
				cs = append(cs, hand[i])
			}
		}
		sortCards(cs)
		melds = append(melds, Meld{Kind: kinds[mi], Cards: cs, Codes: codes(cs)})
	}
	var unmatched []Card
	for i := range hand {
		if used&(1<<uint(i)) == 0 {
			unmatched = append(unmatched, hand[i])
		}
	}
	sortCards(unmatched)
	return Analysis{Deadwood: r.dw, Melds: melds, Unmatched: unmatched}
}

func sortCards(cs []Card) {
	sort.Slice(cs, func(a, b int) bool {
		if cs[a].Suit() != cs[b].Suit() {
			return cs[a].Suit() < cs[b].Suit()
		}
		return cs[a].Rank() < cs[b].Rank()
	})
}

// canLayOff reports whether card c can be appended to meld m, and returns the
// resulting (grown) meld card list if so.
func canLayOff(m Meld, c Card) ([]Card, bool) {
	for _, x := range m.Cards {
		if x == c {
			return nil, false
		}
	}
	if m.Kind == "set" {
		if len(m.Cards) >= 4 {
			return nil, false
		}
		if c.Rank() == m.Cards[0].Rank() {
			return append(append([]Card{}, m.Cards...), c), true
		}
		return nil, false
	}
	// run: same suit, extends either end.
	suit := m.Cards[0].Suit()
	if c.Suit() != suit {
		return nil, false
	}
	lo := m.Cards[0].Rank()
	hi := m.Cards[len(m.Cards)-1].Rank()
	if c.Rank() == lo-1 || c.Rank() == hi+1 {
		grown := append(append([]Card{}, m.Cards...), c)
		sortCards(grown)
		return grown, true
	}
	return nil, false
}

// LayOff greedily lays off deadwood cards onto the given melds (the knocker's
// melds during scoring), returning the reduced deadwood total and the cards
// that were laid off. Highest-value deadwood is laid off first.
func LayOff(deadwood []Card, melds []Meld) (remaining int, laidOff []Card) {
	dw := append([]Card{}, deadwood...)
	sort.Slice(dw, func(a, b int) bool { return dw[a].Value() > dw[b].Value() })
	work := make([]Meld, len(melds))
	copy(work, melds)

	changed := true
	for changed {
		changed = false
		for i := 0; i < len(dw); i++ {
			for mi := range work {
				if grown, ok := canLayOff(work[mi], dw[i]); ok {
					work[mi].Cards = grown
					laidOff = append(laidOff, dw[i])
					dw = append(dw[:i], dw[i+1:]...)
					i--
					changed = true
					break
				}
			}
		}
	}
	for _, c := range dw {
		remaining += c.Value()
	}
	return remaining, laidOff
}
