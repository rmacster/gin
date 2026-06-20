package ginrummy

import "math/rand"

// RobotNames are famous cartoon characters used to name robotic players.
var RobotNames = []string{
	"Bugs Bunny", "Mickey Mouse", "Daffy Duck", "Donald Duck", "Homer Simpson",
	"Bart Simpson", "SpongeBob", "Scooby Doo", "Fred Flintstone", "Tom Cat",
	"Jerry Mouse", "Popeye", "Yogi Bear", "Tweety Bird", "Wile E. Coyote",
	"Road Runner", "Porky Pig", "Pink Panther", "Bender", "Stewie Griffin",
	"Peter Griffin", "Rick Sanchez", "Morty Smith", "Patrick Star", "Velma",
	"Shaggy", "Elmer Fudd", "Marvin Martian", "Foghorn Leghorn", "Snoopy",
}

// RandomRobotName returns a cartoon name not already present in `taken`.
func RandomRobotName(taken map[string]bool) string {
	order := rand.Perm(len(RobotNames))
	for _, i := range order {
		if !taken[RobotNames[i]] {
			return RobotNames[i]
		}
	}
	// All taken: append a number.
	n := RobotNames[rand.Intn(len(RobotNames))]
	return n
}

// RobotAction describes a robot's chosen draw then discard.
type RobotAction struct {
	DrawFromDiscard bool
	Discard         Card
	Knock           bool
}

// DecideDraw chooses where the robot draws from. It takes the discard only if
// that card strictly lowers the robot's best-possible deadwood.
func (g *Game) DecideDraw(idx int) bool {
	p := g.Players[idx]
	top, ok := g.DiscardTop()
	if !ok {
		return false
	}
	base := bestKnockDeadwood(append([]Card{}, p.Hand...)) // deadwood at current hand size after best discard
	withCard := append(append([]Card{}, p.Hand...), top)
	improved := bestKnockDeadwood(withCard)
	// Taking the discard is worthwhile if it reduces achievable deadwood.
	return improved < base
}

// DecideDiscard picks the discard that minimizes the robot's deadwood, and
// decides whether to knock. Assumes the robot has already drawn (oversized hand).
func (g *Game) DecideDiscard(idx int) (Card, bool) {
	p := g.Players[idx]
	bestCard := p.Hand[0]
	bestDead := 1 << 30
	for i := range p.Hand {
		rest := append([]Card{}, p.Hand[:i]...)
		rest = append(rest, p.Hand[i+1:]...)
		d := Analyze(rest).Deadwood
		if d < bestDead {
			bestDead = d
			bestCard = p.Hand[i]
		}
	}
	knock := bestDead <= KnockThreshold
	return bestCard, knock
}
