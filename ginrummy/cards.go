package ginrummy

import (
	"fmt"
	"math/rand"
	"strings"
)

// A Card is encoded as an integer 0..51.
//   rank = card / 4   (0=Ace, 1=2, ... 9=Ten, 10=Jack, 11=Queen, 12=King)
//   suit = card % 4   (0=Clubs, 1=Diamonds, 2=Hearts, 3=Spades)
type Card int

var rankLabels = [13]string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "T", "J", "Q", "K"}
var suitLabels = [4]string{"C", "D", "H", "S"}

func (c Card) Rank() int { return int(c) / 4 }
func (c Card) Suit() int { return int(c) % 4 }

// Value is the deadwood/penalty value: Ace=1, 2..9 face, Ten/Jack/Queen/King=10.
func (c Card) Value() int {
	r := c.Rank() + 1
	if r > 10 {
		return 10
	}
	return r
}

// Code returns a two-character code such as "AS", "TH", "KD" used by the UI.
func (c Card) Code() string {
	return rankLabels[c.Rank()] + suitLabels[c.Suit()]
}

var rankNames = [13]string{"Ace", "2", "3", "4", "5", "6", "7", "8", "9", "10", "Jack", "Queen", "King"}
var suitNames = [4]string{"Clubs", "Diamonds", "Hearts", "Spades"}

// Name returns a human-readable name such as "7 of Hearts" or "King of Spades".
func (c Card) Name() string {
	return rankNames[c.Rank()] + " of " + suitNames[c.Suit()]
}

// ParseCard decodes a two-character code such as "AS" or "th" into a Card.
func ParseCard(code string) (Card, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return 0, fmt.Errorf("invalid card code %q", code)
	}
	rank := -1
	for i, l := range rankLabels {
		if string(code[0]) == l {
			rank = i
			break
		}
	}
	suit := -1
	for i, l := range suitLabels {
		if string(code[1]) == l {
			suit = i
			break
		}
	}
	if rank < 0 || suit < 0 {
		return 0, fmt.Errorf("invalid card code %q", code)
	}
	return Card(rank*4 + suit), nil
}

// NewDeck returns an unshuffled 52-card deck.
func NewDeck() []Card {
	d := make([]Card, 52)
	for i := range d {
		d[i] = Card(i)
	}
	return d
}

// Shuffle randomizes a deck in place.
func Shuffle(d []Card) {
	rand.Shuffle(len(d), func(i, j int) { d[i], d[j] = d[j], d[i] })
}

func codes(cards []Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.Code()
	}
	return out
}
