package ui

import "strings"

type activityRegister uint8

const (
	activityColloquial activityRegister = iota
	activityPlayful
	activityNortheast
	activitySichuan
	activityCantonese
	activityInternet
	activityClassical
	activityHybrid
)

var activityRegisterOrder = [...]activityRegister{
	activityColloquial, activityColloquial, activityColloquial,
	activityColloquial, activityColloquial, activityColloquial,
	activityPlayful, activityPlayful, activityPlayful, activityPlayful,
	activityPlayful, activityPlayful, activityPlayful, activityPlayful,
	activityInternet, activityInternet, activityInternet,
	activityClassical, activityClassical,
	activityNortheast, activitySichuan, activityCantonese,
	activityHybrid,
}

type activityPhrase struct {
	text     string
	register activityRegister
}

func activityPhrases(register activityRegister, text string) []activityPhrase {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	phrases := make([]activityPhrase, 0, len(lines))
	for _, line := range lines {
		phrases = append(phrases, activityPhrase{text: line, register: register})
	}
	return phrases
}

func joinActivityPhrases(groups ...[]activityPhrase) []activityPhrase {
	count := 0
	for _, group := range groups {
		count += len(group)
	}
	phrases := make([]activityPhrase, 0, count)
	for _, group := range groups {
		phrases = append(phrases, group...)
	}
	return phrases
}

var englishActivityPhrases = []string{
	"Pondering the loose thread…",
	"Following the quiet clue…",
	"Checking the hidden hinge…",
	"Tracing a softer path…",
	"Turning the thought over…",
	"Listening for the click…",
	"Peeking behind the obvious…",
	"Testing the side door…",
	"Letting the pieces mingle…",
	"Untangling a small knot…",
	"Reading between the edges…",
	"Trying the other pocket…",
	"Dusting off a clue…",
	"Holding the thought to light…",
	"Checking where it bends…",
	"Following a faint footprint…",
	"Giving the puzzle a nudge…",
	"Looking under the fold…",
	"Measuring the quiet gap…",
	"Taking the scenic route…",
	"Asking the hinge nicely…",
	"Watching the pieces settle…",
	"Comparing two shadows…",
	"Trying a smaller key…",
	"Recounting the breadcrumbs…",
	"Turning down the noise…",
	"Checking the second drawer…",
	"Letting the clue breathe…",
	"Circling the useful bit…",
	"Inspecting the tiny seam…",
	"Balancing the loose pieces…",
	"Walking the thread backward…",
	"Tapping the quiet wall…",
	"Looking past the signpost…",
	"Sorting the shiny bits…",
	"Unfolding one more corner…",
	"Following the warmer trail…",
	"Checking the map upside down…",
	"Asking the gap what it knows…",
	"Giving the knot some room…",
	"Poking the suspicious pebble…",
	"Rearranging the breadcrumbs…",
	"Trying the window latch…",
	"Watching for a small wobble…",
	"Testing the quieter guess…",
	"Turning the lantern slightly…",
	"Finding the useful silence…",
	"Letting the map disagree…",
	"Checking the trail twice…",
	"Following the bent arrow…",
	"Listening at the side door…",
	"Sorting clues by weight…",
	"Trying the key backward…",
	"Reading the smudged margin…",
	"Looking beneath the label…",
	"Counting the missing steps…",
	"Taking a careful detour…",
	"Testing the stubborn corner…",
	"Comparing the quiet parts…",
	"Turning one piece sideways…",
	"Following the thread home…",
	"Checking what stayed still…",
	"Letting the pattern emerge…",
	"Trying a less obvious door…",
	"Watching the edges line up…",
	"Listening for a softer echo…",
	"Moving the lamp a little…",
	"Revisiting the first clue…",
	"Checking the pocket lint…",
	"Tracing the nearly-there path…",
	"Giving the maze a moment…",
	"Looking where nobody pointed…",
	"Turning the clue inside out…",
	"Testing the humble shortcut…",
	"Following a crooked line…",
	"Sorting the useful oddities…",
	"Checking the doorframe twice…",
	"Letting the pieces gossip…",
	"Walking around the assumption…",
	"Listening beneath the noise…",
	"Trying the overlooked lever…",
	"Reading the tiny scratches…",
	"Holding two clues together…",
	"Checking the map legend…",
	"Following the patient route…",
	"Turning the puzzle gently…",
	"Looking behind the footnote…",
	"Testing the almost-fit piece…",
	"Watching the knot loosen…",
	"Counting the quiet signals…",
	"Trying another angle…",
	"Following the useful wrinkle…",
	"Reading the room between lines…",
	"Checking the loose floorboard…",
	"Letting the trail curve…",
	"Pondering one smaller question…",
	"Tracing the clue's shadow…",
	"Trying the handle once more…",
	"Watching the pieces confer…",
	"Listening for the hidden rhyme…",
}
