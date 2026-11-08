package seed

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Faker generates plausible fake values. It is deterministic for a given seed.
type Faker struct{ r *rand.Rand }

func NewFaker(seed int64) *Faker { return &Faker{r: rand.New(rand.NewSource(seed))} }

var (
	firstNames = []string{"Ada", "Alan", "Grace", "Linus", "Margaret", "Dennis", "Barbara", "Ken", "Radia", "Donald", "Frances", "Edsger", "Hedy", "Tim", "Katherine", "Guido"}
	lastNames  = []string{"Lovelace", "Turing", "Hopper", "Torvalds", "Hamilton", "Ritchie", "Liskov", "Thompson", "Perlman", "Knuth", "Allen", "Dijkstra", "Lamarr", "Berners", "Johnson", "Rossum"}
	words      = []string{"lorem", "ipsum", "dolor", "amet", "consectetur", "adipiscing", "elit", "sed", "eiusmod", "tempor", "incididunt", "labore", "magna", "aliqua", "enim", "minim", "veniam", "quis", "nostrud", "ullamco"}
	domains    = []string{"example.com", "example.org", "example.net"}
)

// Int returns an integer in [min, max].
func (f *Faker) Int(min, max int) int {
	if max <= min {
		return min
	}
	return min + f.r.Intn(max-min+1)
}

// Float returns a float in [min, max).
func (f *Faker) Float(min, max float64) float64 { return min + f.r.Float64()*(max-min) }

func (f *Faker) Bool() bool { return f.r.Intn(2) == 1 }

// Pick returns one of the options; the empty string when none are given.
func (f *Faker) Pick(options ...string) string {
	if len(options) == 0 {
		return ""
	}
	return options[f.r.Intn(len(options))]
}

func (f *Faker) FirstName() string { return f.Pick(firstNames...) }
func (f *Faker) LastName() string  { return f.Pick(lastNames...) }
func (f *Faker) Name() string      { return f.FirstName() + " " + f.LastName() }
func (f *Faker) Word() string      { return f.Pick(words...) }

func (f *Faker) Words(n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = f.Word()
	}
	return strings.Join(out, " ")
}

func (f *Faker) Sentence() string {
	s := f.Words(f.Int(5, 10))
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

func (f *Faker) Paragraph() string {
	n := f.Int(3, 5)
	out := make([]string, n)
	for i := range out {
		out[i] = f.Sentence()
	}
	return strings.Join(out, " ")
}

// Username is lowercase with a numeric suffix to reduce collisions.
func (f *Faker) Username() string {
	return fmt.Sprintf("%s%s%d", strings.ToLower(f.FirstName()), strings.ToLower(f.LastName()[:3]), f.Int(1, 9999))
}

// Email uses reserved example domains so generated data never reaches real mailboxes.
func (f *Faker) Email() string { return f.Username() + "@" + f.Pick(domains...) }

// UUID returns a random version-4 UUID.
func (f *Faker) UUID() string {
	var b [16]byte
	for i := range b {
		b[i] = byte(f.r.Intn(256))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Date returns a time between from and to.
func (f *Faker) Date(from, to time.Time) time.Time {
	if !to.After(from) {
		return from
	}
	return from.Add(time.Duration(f.r.Int63n(int64(to.Sub(from)))))
}
