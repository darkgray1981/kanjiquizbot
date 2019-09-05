package main

import (
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const TestQuiz = "quizvalidate_test"

func TestStringSet(t *testing.T) {
	set := NewStringSet()

	// Test Add and AddAll
	set.AddAll("a", "a", "b")
	set.Add("c")
	set.Add("c")
	add := NewStringSet()
	add.AddAll("a", "b", "c")
	if !cmp.Equal(set.Values(), add.Values(), cmpopts.SortSlices(func(x, y string) bool { return x < y })) {
		t.Errorf("error:%+v != %+v\n", set.Values(), add.Values())
	}

	// Test Remove
	set.Remove("c")
	remove := NewStringSet()
	remove.AddAll("a", "b")
	if !cmp.Equal(set.Values(), remove.Values(), cmpopts.SortSlices(func(x, y string) bool { return x < y })) {
		t.Errorf("error:%+v != %+v\n", set.Values(), remove.Values())
	}

	// Test IsEmpty
	set.Remove("a")
	set.Remove("b")
	empty := NewStringSet()
	if !set.IsEmpty() || !empty.IsEmpty() {
		t.Error("IsEmpty() should be true")
	}

	// Test order preservation
	set.Add("a")
	set.Add("b")
	set.AddOrdered("d", 3)
	set.AddOrdered("c", 2)
	order := NewStringSet()
	order.AddAll("a", "b", "c", "d")
	if !cmp.Equal(set.Values(), order.Values()) {
		t.Errorf("error:%+v != %+v\n", set.Values(), order.Values())
	}
}

func TestValidations(t *testing.T) {

	// Weird to initialize a global in a test like this
	// Would be better to eventually have another less specific test covering the bot's initialization
	loadQuizList()
	Quizzes.Map[TestQuiz] = "_" + TestQuiz + ".json"
	fixedQuizPath := QUIZ_FOLDER + Quizzes.Map[TestQuiz] + ".fix"

	// Raw strings and indentation don't go together
	correctQuizRaw := `{
	"description": "Test quiz with duplicates",
	"type": "text",
	"deck": [
		{ "question": "q1", "answers": [ "aaa", "bbb" ], "comment": "c1\nc2" },
		{ "question": "q2", "answers": [ "ccc", "ddd", "eee" ], "comment": "c3" }
	]
}`
	correctQuiz := createTestQuiz(correctQuizRaw)

	// Actual validation logic is tested below
	// This test covers the genereation of fixed quiz copies
	ValidateQuizzes(GetQuizlist(), true)
	var fixedQuiz Quiz
	f, err := os.Open(fixedQuizPath)
	if err != nil {
		log.Fatal(err)
	}
	json.NewDecoder(f).Decode(&fixedQuiz)
	f.Close()

	// Ignoring comment field because supporting field-specific comparison behavior is too annoying
	if !quizEqual(correctQuiz, fixedQuiz, cmpopts.IgnoreFields(Card{}, "Comment")) {
		t.Errorf("Check duplicates failed! %+v != %+v", correctQuiz, fixedQuiz)
	}
	// Clean up
	os.Remove(fixedQuizPath)
}

func TestDuplicateValidation(t *testing.T) {

	// Raw strings and indentation don't go together
	dedupQuizRaw := `{
	"description": "Test quiz with duplicates",
	"type": "text",
	"deck": [
		{ "question": "q1", "answers": [ "aaa", "bbb" ], "comment": "c1\nc2" },
		{ "question": "q2", "answers": [ "ccc", "ddd", "eee" ], "comment": "c3" }
	]
}`

	var dupQuiz Quiz
	f, err := os.Open(QUIZ_FOLDER + "_" + TestQuiz + ".json")
	if err != nil {
		log.Fatal(err)
	}
	json.NewDecoder(f).Decode(&dupQuiz)
	f.Close()

	dedupQuiz := createTestQuiz(dedupQuizRaw)
	fixedQuiz, _ := checkDuplicates(dupQuiz)

	// Ignoring comment field because supporting field-specific comparison behavior is too annoying
	if !quizEqual(dedupQuiz, fixedQuiz, cmpopts.IgnoreFields(Card{}, "Comment")) {
		t.Errorf("Check duplicates failed! %+v != %+v", dedupQuiz, fixedQuiz)
	}
}

func createTestQuiz(raw string) (quiz Quiz) {
	err := json.Unmarshal([]byte(raw), &quiz)
	if err != nil {
		log.Fatal(err)
	}
	return
}

// Helper function for comparing quizzes
// Using custom compare because reflect.DeepEqual() cannot correctly equate slices with different order
func quizEqual(qx, qy Quiz, additionalOpts ...cmp.Option) bool {
	opts := []cmp.Option{
		cmpopts.SortSlices(func(x, y string) bool { return x < y }),
		cmpopts.SortSlices(func(x, y Card) bool { return x.Question < y.Question }),
	}
	opts = append(opts, additionalOpts...)
	return cmp.Equal(qx, qy, opts...)
}
