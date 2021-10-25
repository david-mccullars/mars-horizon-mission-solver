package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/david-mccullars/mars-horizon-mission-solver/parallelsearch"
	"github.com/gookit/color"
)

/////////////////////////////////////////////////////////////////////////////////////////////////////

// Resources represents a state or goal in the Mars Horizons mini-game
type Resources struct {
	Comm      int
	Data      int
	Nav       int
	Power     int
	Drift     int
	Heat      int
	Thrust    int
	Crew      int
	Radiation int
}

func (self *Resources) add(other *Resources) {
	self.Comm += other.Comm
	self.Data += other.Data
	self.Nav += other.Nav
	self.Power += other.Power
	self.Drift += other.Drift
	self.Heat += other.Heat
	self.Thrust += other.Thrust
	self.Crew += other.Crew
	self.Radiation += other.Radiation
}

func (self *Resources) subtract(other *Resources) {
	self.Comm -= other.Comm
	self.Data -= other.Data
	self.Nav -= other.Nav
	self.Power -= other.Power
	self.Drift -= other.Drift
	self.Heat -= other.Heat
	self.Thrust -= other.Thrust
	self.Crew -= other.Crew
	self.Radiation -= other.Radiation
}

func (self *Resources) endsWithin(lowerBound *Resources, upperBound *Resources) bool {
	return self.Comm > lowerBound.Comm && self.Comm < upperBound.Comm &&
		self.Data > lowerBound.Data && self.Data < upperBound.Data &&
		self.Nav > lowerBound.Nav && self.Nav < upperBound.Nav &&
		self.Power > lowerBound.Power && self.Power < upperBound.Power &&
		self.Drift > lowerBound.Drift && self.Drift < upperBound.Drift &&
		self.Heat > lowerBound.Heat && self.Heat < upperBound.Heat &&
		self.Thrust > lowerBound.Thrust && self.Thrust < upperBound.Thrust &&
		self.Crew > lowerBound.Crew && self.Crew < upperBound.Crew &&
		self.Radiation > lowerBound.Radiation && self.Radiation < upperBound.Radiation
}

func (self *Resources) risk(goal *Resources) int {
	risk := 10*self.Power - 100*self.Radiation
	if goal.Comm > 0 {
		risk += self.Comm - goal.Comm
	}
	if goal.Data > 0 {
		risk += self.Data - goal.Data
	}
	if goal.Nav > 0 {
		risk += self.Nav - goal.Nav
	}
	if goal.Thrust > 0 {
		risk += self.Thrust - goal.Thrust
	}
	// Ignore Drift, Heat, & Crew
	return risk
}

func (self *Resources) String() string {
	e := []string{}
	if self.Comm > 0 {
		e = append(e, "comm: "+colorize("red", self.Comm))
	}
	if self.Data > 0 {
		e = append(e, "data: "+colorize("cyan", self.Data))
	}
	if self.Nav > 0 {
		e = append(e, "nav: "+colorize("magenta", self.Nav))
	}
	if self.Power > 0 {
		e = append(e, "power: "+colorize("yellow", self.Power))
	}
	if self.Drift != 0 {
		e = append(e, "drift: "+colorize("green", self.Drift))
	}
	if self.Heat > 0 {
		e = append(e, "heat: "+colorize("red", self.Heat))
	}
	if self.Thrust > 0 {
		e = append(e, "thrust: "+colorize("white", self.Thrust))
	}
	if self.Crew > 0 {
		e = append(e, "crew: "+colorize("white", self.Crew))
	}
	if self.Radiation > 0 {
		e = append(e, "radiation: "+colorize("green", self.Radiation))
	}
	return strings.Join(e[:], " | ")
}

/////////////////////////////////////////////////////////////////////////////////////////////////////

// Command is an action that can be taken that requires certain input and produces certain output
type Command struct {
	Name   string
	Input  Resources
	Output Resources
}

/////////////////////////////////////////////////////////////////////////////////////////////////////

// Scenario is a specific Mars Horizons mini-game scenario with a starting set of resources, a set of
// commands, and a desired goal
type Scenario struct {
	Turns            uint32
	ActionsPerTurn   uint32 `json:"actions_per_turn"`
	Start            Resources
	Goal             Resources
	Commands         []Command
	TurnCost         Resources `json:"turn_cost"`
	TurnMustEndAbove Resources `json:"turn_must_end_above"`
	TurnMustEndBelow Resources `json:"turn_must_end_below"`
}

func (self *Scenario) totalActions() uint32 {
	return self.Turns * self.ActionsPerTurn
}

func (self *Scenario) findCommand(name string) *Command {
	for _, c := range self.Commands {
		if c.Name == name {
			return &c
		}
	}
	return nil
}

func copyFileIfNotExist(src string, dst string) {
	_, err := os.Stat(dst)
	if !os.IsNotExist(err) {
		return
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		log.Fatal(err)
	}

	from, err := os.Open(src)
	if err != nil {
		log.Fatal(err)
	}
	defer from.Close()

	to, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE, srcInfo.Mode())
	if err != nil {
		log.Fatal(err)
	}
	defer to.Close()

	_, err = io.Copy(to, from)
	if err != nil {
		log.Fatal(err)
	}
}

func loadScenario() *Scenario {
	copyFileIfNotExist("example-scenario.yml", "scenario.yml")

	cmd := exec.Command("sh", "-c", "vim scenario.yml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	rawJSON := &strings.Builder{}
	cmd = exec.Command("scenario_from_shorthand", "scenario.yml")
	cmd.Stdout = rawJSON
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	scenario := Scenario{}
	json.Unmarshal([]byte(rawJSON.String()), &scenario)
	return &scenario
}

/////////////////////////////////////////////////////////////////////////////////////////////////////

// Sequence is a list of commands that have been run with the state of resources arrived at by these
// commands
type Sequence struct {
	scenario  *Scenario
	Resources *Resources
	Command   *Command
	Prev      *Sequence
	Size      uint32
}

func (self *Sequence) commandName() string {
	if self.Size == 0 {
		return "[START]"
	}
	return strings.ToUpper(self.Command.Name)
}

func (self *Sequence) commandSequence() string {
	if self.Size == 0 {
		return self.commandName()
	}
	stack := []string{}
	for prev := self; prev != nil && prev.Size > 0; prev = prev.Prev {
		stack = append([]string{prev.commandName()}, stack...)
	}
	return strings.Join(stack[:], " -> ")
}

func (self *Sequence) printSummary() {
	fmt.Println()
	fmt.Println(colorize("yellow", "################################################################################"))
	fmt.Println()
	stack := []*Sequence{}
	for prev := self; prev != nil && prev.Size > 0; prev = prev.Prev {
		stack = append([]*Sequence{prev}, stack...)
	}
	for turn := uint32(1); turn <= self.scenario.Turns; turn++ {
		commands := []string{}
		var last *Sequence
		for action := uint32(1); action <= self.scenario.ActionsPerTurn; action++ {
			if len(stack) == 0 {
				break
			}
			last = stack[0]
			stack = stack[1:]
			commands = append(commands, colorize("red", last.commandName()))
		}
		if last == nil {
			return
		}
		fmt.Println(colorize("gray", "[", turn, "]"), strings.Join(commands[:], " -> "))
		fmt.Println("\t", last.Resources)
	}
}

func (self *Sequence) isNewTurn() bool {
	return self.Size%self.scenario.ActionsPerTurn == 1
}

func (self *Sequence) isTurnEnd() bool {
	return self.Size%self.scenario.ActionsPerTurn == 0
}

func (self *Sequence) hasMoreActionsAvailable() bool {
	return self.Size < self.scenario.totalActions()
}

func (self *Sequence) isInvalid() bool {
	if self.isTurnEnd() && !self.Resources.endsWithin(&self.scenario.TurnMustEndAbove, &self.scenario.TurnMustEndBelow) {
		return true
	}

	// Ignore Drift, Thrust, & Radiation
	return self.Resources.Comm < 0 ||
		self.Resources.Data < 0 ||
		self.Resources.Nav < 0 ||
		self.Resources.Power < 0 ||
		self.Resources.Heat < 0 ||
		self.Resources.Crew < 0
}

func (self *Sequence) isSuccess() bool {
	goal := self.scenario.Goal
	// Ignore Heat & Radiation
	return self.Resources.Comm >= goal.Comm &&
		self.Resources.Data >= goal.Data &&
		self.Resources.Nav >= goal.Nav &&
		self.Resources.Power >= goal.Power &&
		self.Resources.Drift >= -goal.Drift && self.Resources.Drift <= goal.Drift &&
		(self.Resources.Thrust >= goal.Thrust || goal.Thrust == 0)
}

func (self *Sequence) attemptAction(command *Command) *Sequence {
	resources := *self.Resources // Make a copy to allow for mutation
	next := Sequence{self.scenario, &resources, command, self, self.Size + 1}

	// Apply any logic at the beginning of a new turn (not including the first turn)
	if next.Size > 1 && next.isNewTurn() {
		if self.scenario.Start.Crew > 0 {
			next.Resources.Crew = self.scenario.Start.Crew
		}
		next.Resources.add(&self.scenario.TurnCost)
	}

	next.Resources.subtract(&command.Input)

	if next.isInvalid() {
		return nil
	}

	next.Resources.add(&command.Output)

	if next.isInvalid() {
		return nil
	}

	return &next
}

func (self *Sequence) playActions(commands ...string) {
	seq := self
	fmt.Println("START: ", seq.Resources)
	for _, name := range commands {
		command := self.scenario.findCommand(name)
		if command == nil {
			log.Fatal("Invalid command: " + name)
		}
		seq = seq.attemptAction(command)
		if seq == nil {
			log.Fatal("Can not take action: " + name)
		}
		seq.printSummary()
	}
}

// Search implements Searchable interface for continuing the search from this sequence into a
// subsequence sequence by taking an available (and legal) action
func (self *Sequence) Search(onNext func(parallelsearch.Searchable)) {
	if self.hasMoreActionsAvailable() {
		for i := range self.scenario.Commands {
			command := self.scenario.Commands[i] // WARNING: Be careful about reusing a variable from range that gets passed by value
			next := self.attemptAction(&command)
			if next != nil {
				onNext(next)
			}
		}
	}
}

// IsFound implements Searchable interface to determine if the current sequence meets the goal
// we are looking for
func (self *Sequence) IsFound() bool {
	return self.isSuccess()
}

// Score implements Searchable interface and provides the ability to sort the discovered solutions
// to try and present the "best" solution last.  We consider sequences that are shorter to be the
// least "risky" (since we have more wiggle room to fix things if actions fail).  If two sequences
// have the same size, we prefer the ones that leave us with the most resources (especially power).
func (self *Sequence) Score() int {
	return int(self.Size*1000) - self.Resources.risk(&self.scenario.Goal)
}

func startSequence(scenario *Scenario) *Sequence {
	start := Sequence{scenario, &scenario.Start, nil, nil, 0}
	return &start
}

/////////////////////////////////////////////////////////////////////////////////////////////////////

func colorize(colorName string, a ...interface{}) string {
	s := fmt.Sprint(a...)
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return color.Sprint("<", colorName, ">", s, "</>")
	}
	return s
}

func main() {
	runtime.GOMAXPROCS(16)

	scenario := loadScenario()
	startSequence := startSequence(scenario)

	// Rather than perform a search, it is possible to specify a list of actions,
	// and this will show each step and what the resources look like after each one.
	if len(os.Args) > 1 {
		startSequence.playActions(os.Args[1:]...)
		return
	}

	ps := parallelsearch.New(
		128,                          // poolSize
		int(scenario.totalActions()), // searchDepth
		4,                            // searchLimit
	)
	ps.Start(startSequence)

	found := ps.WaitForFound()
	for _, s := range found {
		sequence := s.(*Sequence)
		sequence.printSummary()
	}
}
