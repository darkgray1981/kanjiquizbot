package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"sort"
	"strings"
)

// SortedStringSet is originally a simple implementation of a set of strings
// Since the order of answers should be preserved, this set-modoki can now take in an index as value for
// its underlying map. When the values (map's keyset) are returned, they will be sorted based on the assigned
// value
//
// *TODO* evaluate merits of breaking this out to its own file when validations list grows too long
type SortedStringSet struct {
	content map[string]int
}

// NewStringSet initializes set
func NewStringSet() *SortedStringSet {
	return &SortedStringSet{make(map[string]int)}
}

// Add inserts string into set and returns boolean for change
func (set *SortedStringSet) Add(s string) bool {
	return set.AddOrdered(s, len(set.content))
}

// AddOrdered takes in a string and an integer to identify its position
func (set *SortedStringSet) AddOrdered(s string, i int) bool {
	_, exists := set.content[s]
	set.content[s] = i
	return !exists
}

// AddAll inserts multiple string elements into set
// Returns any strings that already exist in set (unconventional, but I want to print them)
func (set *SortedStringSet) AddAll(strs ...string) []string {
	var dups []string
	for i, s := range strs {
		changed := set.AddOrdered(s, i)
		if !changed {
			dups = append(dups, s)
		}
	}
	return dups
}

// Remove removes given string from set
func (set *SortedStringSet) Remove(s string) {
	delete(set.content, s)
}

// IsEmpty checks the set's emptiness by evaluating content's zero value
func (set SortedStringSet) IsEmpty() bool {
	return set.content == nil || len(set.content) == 0
}

// Values returns a slice of set members
func (set SortedStringSet) Values() []string {
	keys := make([]string, len(set.content))
	i := 0
	for key := range set.content {
		keys[i] = key
		i++
	}
	sort.Slice(keys, func(i, j int) bool {
		return set.content[keys[i]] < set.content[keys[j]]
	})
	return keys
}

// ValidateQuizzes will run through the following checks:
//   checkDuplicates - validates duplicate questions and answers
//
// Parameter quizNames defines the quizzes to be checked
// Parameter generateFix is a boolean that controls the creation of fixed quiz copies
func ValidateQuizzes(quizNames []string, generateFix bool) {

	quizzes := make(chan string, len(quizNames))
	done := make(chan string, len(quizNames))

	for w := 0; w < runtime.NumCPU(); w++ {
		go quizValidationWorker(quizzes, done, generateFix)
	}

	for _, quizName := range quizNames {
		quizzes <- quizName
	}
	close(quizzes)

	for w := 0; w < len(quizNames); w++ {
		q := <-done
		log.Printf("[%s] Validation complete\n", q)
	}
}

func quizValidationWorker(quizzes <-chan string, done chan<- string, generateFix bool) {
	for quizName := range quizzes {
		quiz := LoadQuiz(quizName, true)
		log.Printf("[%s] Running checks...\n", quizName)

		// Run checks
		//
		// *TODO* look into making a validator interface and move specific validation logic into structs when the list
		//        of validations grows too long, or if some have particularly complex logic
		fixed, hasError := checkDuplicates(quiz)

		if hasError && generateFix {

			// Create a copy of quiz file
			// Delete if exists
			fileName := QUIZ_FOLDER + Quizzes.Map[quizName] + ".fix"
			if _, err := os.Stat(fileName); !os.IsNotExist(err) {
				err := os.Remove(fileName)
				if err != nil {
					log.Fatal(err)
				}
			}
			f, err := os.Create(fileName)
			if err != nil {
				log.Fatal(err)
			}
			w := bufio.NewWriter(f)

			// Write quiz JSON file
			buf := new(bytes.Buffer)
			enc := json.NewEncoder(buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(&fixed); err != nil {
				log.Fatal(err)
			}

			// Indent the JSON manually
			j := strings.NewReplacer(
				`"description":`, "\n\t"+`"description": `,
				`"type":`, "\n\t"+`"type": `,
				`"timeout":`, "\n\t"+`"timeout": `,
				`"deck":[`, "\n\t"+`"deck": [`,
				`{"question":`, "\n\t\t"+`{ "question": `,
				`,"answers":[`, `, "answers": [ `,
				`],"comment":`, ` ], "comment": `,
				`}]}`, "}\n\t]\n}",
			).Replace(buf.String())
			strings.NewReplacer(
				`]}`, `] }`,
				`"}`, `" }`,
				`"]`, `" ]`,
			).WriteString(w, j)
			w.Flush()
			f.Close()
			log.Printf("[%s] Generated fixed file %s\n", quizName, fileName)
		}

		done <- quizName
	}
}

// Checks duplicate questions and answers in a given quiz
// Currently the strategy is to merge the answers and comments for cards with the same question
// Returns fixed quiz
func checkDuplicates(quiz Quiz) (Quiz, bool) {
	log.Println("Checking duplicates...")
	var hasError bool

	// Use a map to hold merged card data temporarily
	cardMap := make(map[string][]*SortedStringSet)
	for _, card := range quiz.Deck {
		question := card.Question

		var cardDataSets []*SortedStringSet
		if cardMap[question] != nil {
			cardDataSets = cardMap[question]
			log.Printf("\tFound duplicate question: %s", question)
			hasError = true
		} else {
			cardDataSets = []*SortedStringSet{NewStringSet(), NewStringSet()}
		}
		cardDataSets[1].Add(card.Comment)

		dups := cardDataSets[0].AddAll(card.Answers...)
		if dups != nil {
			hasError = true
			log.Printf("\tFound duplicate answers: %s\n", strings.Join(dups, ", "))
		}
		cardMap[question] = cardDataSets
	}

	// Populate quiz deck with fixed cards
	fixedDeck := make([]Card, len(cardMap))
	i := 0
	for question, cardDataSets := range cardMap {
		fixedDeck[i] = Card{
			Question: question,
			Answers:  cardDataSets[0].Values(),
			Comment:  strings.Join(cardDataSets[1].Values(), "\n"),
		}
		i++
	}
	sort.Slice(fixedDeck, func(i, j int) bool {

		// Since we're deduping, the questions should never be equal
		if fixedDeck[i].Question < fixedDeck[j].Question {
			return true
		}
		return false
	})

	return Quiz{
		Description: quiz.Description,
		Type:        quiz.Type,
		Timeout:     quiz.Timeout,
		Deck:        fixedDeck,
	}, hasError
}
