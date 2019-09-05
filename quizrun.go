package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Run kanji quiz loop in given channel
func runQuiz(s *discordgo.Session, quizChannel string, quizname string, winLimitGiven string, waitTimeGiven int, pauseTimeGiven int) {

	// Mark the quiz as started
	if err := startQuiz(s, quizChannel); err != nil {
		// Quiz already running, nothing to do here
		return
	}

	winLimit := 15                                                // winner score
	timeout := 20                                                 // seconds to wait per round
	timeoutLimit := 5                                             // count before aborting
	pauseTime := time.Duration(pauseTimeGiven) * time.Millisecond // delay before next question

	// Set delay before closing round
	waitTime := time.Duration(waitTimeGiven) * time.Millisecond

	var quiz Quiz
	if quizname == "review" {
		quiz = getReview(quizChannel)
		winLimit = len(quiz.Deck)
	} else {
		quiz = LoadQuiz(quizname, true)
	}
	if len(quiz.Deck) == 0 {
		msgSend(s, quizChannel, "Failed to find valid quiz: "+quizname)
		stopQuiz(s, quizChannel)
		return
	}

	// Parse provided winLimit with sane defaults
	if i, err := strconv.Atoi(winLimitGiven); err == nil {
		if i > len(quiz.Deck) {
			i = len(quiz.Deck)
		}

		if i > 100 {
			winLimit = 100
		} else if i < 1 {
			winLimit = 1
		} else {
			winLimit = i
		}
	}

	// Replace default timeout with custom if specified
	if quiz.Timeout > 0 {
		timeout = quiz.Timeout
	}

	c := make(chan *discordgo.MessageCreate, 100)
	quitChan := make(chan struct{}, 100)

	killHandler := s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by self and bots
		if m.Author.ID == s.State.User.ID || m.Author.Bot {
			return
		}

		// Only react on current quiz channel
		if m.ChannelID != quizChannel {
			return
		}

		// Handle quiz aborts
		if strings.ToLower(strings.TrimSpace(m.Content)) == CMD_PREFIX+"stop" {
			quitChan <- struct{}{}
			return
		}

		// Relay the message to the quiz loop
		c <- m
	})

	msgSend(s, quizChannel, fmt.Sprintf("```Starting new %s quiz (%d questions) in %.f seconds:\n\"%s\"\nFirst to %d points wins.```", quizname, len(quiz.Deck), float64(pauseTime/time.Second), quiz.Description, winLimit))

	var quizHistory []string
	var failed []Card
	var questionTitle string
	players := make(map[string]int)
	var timeoutCount int

outer:
	for len(quiz.Deck) > 0 {
		time.Sleep(pauseTime)

		// Grab new word from the quiz
		var current Card
		current, quiz.Deck = quiz.Deck[len(quiz.Deck)-1], quiz.Deck[:len(quiz.Deck)-1]

		// Replace readings with hiragana-only version
		answers := make([]string, len(current.Answers))
		for i, ans := range current.Answers {
			answers[i] = k2h(ans)
		}

		// Add word to quiz history
		if (quiz.Type == "text" || quiz.Type == "url") && len(current.Answers) > 0 {
			quizHistory = append(quizHistory, current.Answers[0])
			questionTitle = ""
		} else {
			quizHistory = append(quizHistory, current.Question)
			questionTitle = truncate(current.Question, 100)
		}

		// Round's score keeper
		scoreKeeper := make(map[string]int)

		// Drain premature "answers" from channel buffer
		for len(c) > 0 {
			<-c
		}

		// Send out quiz question
		if quiz.Type == "text" {
			msgSend(s, quizChannel, fmt.Sprintf("```\n%s```", current.Question))
		} else if quiz.Type == "url" {
			msgSend(s, quizChannel, current.Question)
		} else {
			imgSend(s, quizChannel, current.Question)
		}

		// Set timeout for no correct answers
		timeoutChan := time.NewTimer(time.Duration(timeout) * time.Second)

	inner:
		for {

			select {
			case <-quitChan:
				// Quit order received, but store remaining questions for reviews
				if quizname == "review" {
					// Did this question get answered
					if len(scoreKeeper) == 0 {
						// Store question for later review deck
						failed = append(failed, current)
					}

					// Store unused review questions
					failed = append(failed, quiz.Deck...)
				}
				break outer
			case <-timeoutChan.C:
				if len(scoreKeeper) > 0 {
					break inner
				}

				embed := &discordgo.MessageEmbed{
					Type:        "rich",
					Title:       fmt.Sprintf(UNICODE_NO_ENTRY+" Timed out! %s", questionTitle),
					Description: fmt.Sprintf("**%s**", truncate(strings.Join(current.Answers, ", "), 2000)),
					Color:       0xAA2222,
				}

				if len(current.Comment) > 0 {
					embed.Fields = []*discordgo.MessageEmbedField{
						&discordgo.MessageEmbedField{
							Name:   "Comment",
							Value:  truncate(current.Comment, 1024),
							Inline: false,
						}}
				}

				embedSend(s, quizChannel, embed)

				// Store question for later review deck
				failed = append(failed, current)

				// Mark latest question as failed in quiz history as well
				quizHistory[len(quizHistory)-1] = "*" + quizHistory[len(quizHistory)-1]

				timeoutCount++
				if timeoutCount >= timeoutLimit {
					msgSend(s, quizChannel, "```Too many timeouts in a row reached, aborting quiz.```")

					if quizname == "review" {
						// Store unused review questions
						failed = append(failed, quiz.Deck...)
					}

					break outer
				}
				break inner
			case msg := <-c:
				// Handle passing on question
				if msg.Content == ".." || msg.Content == "。。" {

					// Abort the question
					timeoutChan.Reset(0)

				} else if hasString(answers, k2h(msg.Content)) {
					if len(scoreKeeper) == 0 {
						timeoutChan.Reset(waitTime)
					}

					// Make sure we don't add the same user again
					if _, exists := scoreKeeper[msg.Author.ID]; !exists {
						scoreKeeper[msg.Author.ID] = len(scoreKeeper) + 1
					}

					// Reset timeouts since we're active
					timeoutCount = 0
				}
			}
		}

		if len(scoreKeeper) > 0 {

			winnerExists := false
			var fastest string
			var scorers []string
			for player, position := range scoreKeeper {
				players[player]++
				if position == 1 {
					fastest = fmt.Sprintf("<@%s> %dp", player, players[player])
				} else {
					scorers = append(scorers, fmt.Sprintf("<@%s> %dp", player, players[player]))
				}
				if players[player] >= winLimit {
					winnerExists = true
				}
			}

			scorers = append([]string{fastest}, scorers...)

			embed := &discordgo.MessageEmbed{
				Type:        "rich",
				Title:       fmt.Sprintf(UNICODE_CHECK_MARK+" Correct: %s", questionTitle),
				Description: fmt.Sprintf("**%s**", truncate(strings.Join(current.Answers, ", "), 2000)),
				Color:       0x22AA22,
				Fields: []*discordgo.MessageEmbedField{
					&discordgo.MessageEmbedField{
						Name:   fmt.Sprintf("Scorers - %s to %d", quizname, winLimit),
						Value:  strings.Join(scorers, ", "),
						Inline: false,
					}},
			}

			if len(current.Comment) > 0 {
				embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
					Name:   "Comment",
					Value:  truncate(current.Comment, 1024),
					Inline: false,
				})
			}

			embedSend(s, quizChannel, embed)

			if winnerExists {
				break outer
			}
		}

	}

	// Clean up
	killHandler()

	// Produce scoreboard
	fields := make([]*discordgo.MessageEmbedField, 0, 2)
	var winners string
	var participants string

	for _, p := range ranking(players) {
		if p.Score >= winLimit && quizname != "review" {
			winners += fmt.Sprintf("<@%s>: %d points\n", p.Name, p.Score)
		} else {
			participants += fmt.Sprintf("<@%s>: %d point(s)\n", p.Name, p.Score)
		}
	}

	if len(winners) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Winner",
			Value:  winners,
			Inline: false,
		})
	}

	if len(participants) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Participants",
			Value:  participants,
			Inline: false,
		})
	}

	if len(failed) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Note",
			Value:  fmt.Sprintf("Try `%squiz review` to replay the %d failed question(s)\n", CMD_PREFIX, len(failed)),
			Inline: false,
		})
	}

	// Sleep for a little breathing room
	time.Sleep(1 * time.Second)

	embed := &discordgo.MessageEmbed{
		Type:        "rich",
		Title:       "Final Quiz Scoreboard: " + quizname,
		Description: "-------------------------------",
		Color:       0x33FF33,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: truncate(strings.Join(quizHistory, "　"), 2000)},
	}

	embedSend(s, quizChannel, embed)

	// Store review questions in memory
	quiz.Deck = failed
	putReview(quizChannel, copyQuiz(quiz))

	stopQuiz(s, quizChannel)
}

// Run multi quiz loop in given channel
func runMultiQuiz(s *discordgo.Session, quizChannel string, quizname string, winLimitGiven string, waitTimeGiven int, pauseTimeGiven int) {

	// Mark the quiz as started
	if err := startQuiz(s, quizChannel); err != nil {
		// Quiz already running, nothing to do here
		return
	}

	winLimit := 15                                                // winner score
	timeout := 13                                                 // seconds to wait per round
	timeoutLimit := 5                                             // count before aborting
	pauseTime := time.Duration(pauseTimeGiven) * time.Millisecond // delay before next question
	pointLimit := 3                                               // possible points per question

	// Set delay before closing round
	waitTime := time.Duration(waitTimeGiven) * time.Millisecond

	quiz := LoadQuiz(quizname, true)
	if len(quiz.Deck) == 0 {
		msgSend(s, quizChannel, "Failed to find quiz: "+quizname)
		stopQuiz(s, quizChannel)
		return
	}

	// Parse provided winLimit with sane defaults
	if i, err := strconv.Atoi(winLimitGiven); err == nil {
		if i > len(quiz.Deck) {
			i = len(quiz.Deck)
		}

		if i > 100 {
			winLimit = 100
		} else if i < 1 {
			winLimit = 1
		} else {
			winLimit = i
		}
	}

	c := make(chan *discordgo.MessageCreate, 100)
	quitChan := make(chan struct{}, 100)

	killHandler := s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by self and bots
		if m.Author.ID == s.State.User.ID || m.Author.Bot {
			return
		}

		// Only react on current quiz channel
		if m.ChannelID != quizChannel {
			return
		}

		// Handle quiz aborts
		if strings.ToLower(strings.TrimSpace(m.Content)) == CMD_PREFIX+"stop" {
			quitChan <- struct{}{}
			return
		}

		// Relay the message to the quiz loop
		c <- m
	})

	msgSend(s, quizChannel, fmt.Sprintf("```Starting new %s MULTI quiz (%d questions) in %.f seconds:\n\"%s\"\nFirst to %d points wins.```", quizname, len(quiz.Deck), float64(pauseTime/time.Second), quiz.Description, winLimit))

	var quizHistory []string
	var questionTitle string
	players := make(map[string]int)
	var timeoutCount int

outer:
	for len(quiz.Deck) > 0 {
		time.Sleep(pauseTime)

		// Grab new word from the quiz
		var current Card
		current, quiz.Deck = quiz.Deck[len(quiz.Deck)-1], quiz.Deck[:len(quiz.Deck)-1]
		answerMap := make(map[string]time.Time)

		// Populate answer map with lowercase/hiragana-reading version
		for _, ans := range current.Answers {
			// Initialize with zero time
			answerMap[k2h(strings.ToLower(ans))] = time.Time{}
		}
		answersLeft := len(answerMap)

		// Add word to quiz history
		if (quiz.Type == "text" || quiz.Type == "url") && len(current.Answers) > 0 {
			quizHistory = append(quizHistory, current.Answers[0])
			questionTitle = ""
		} else {
			quizHistory = append(quizHistory, current.Question)
			questionTitle = truncate(current.Question, 100)
		}

		// Round's score keeper
		scoreKeeper := make(map[string]int)
		scoreKeeperAnswers := make(map[string][]string)

		// Drain premature "answers" from channel buffer
		for len(c) > 0 {
			<-c
		}

		// Send out quiz question
		if quiz.Type == "text" {
			msgSend(s, quizChannel, fmt.Sprintf("```\n%s```", current.Question))
		} else if quiz.Type == "url" {
			msgSend(s, quizChannel, current.Question)
		} else {
			imgSend(s, quizChannel, current.Question)
		}

		// Set timeout for no correct answers
		bonusTime := minint(len(current.Answers)*2, 12)
		timeoutChan := time.NewTimer(time.Duration(timeout+bonusTime) * time.Second)

	inner:
		for {

			select {
			case <-quitChan:
				break outer
			case <-timeoutChan.C:
				if len(scoreKeeper) > 0 {
					break inner
				}

				embed := &discordgo.MessageEmbed{
					Type:        "rich",
					Title:       fmt.Sprintf(UNICODE_NO_ENTRY+" Timed out! %s", questionTitle),
					Description: fmt.Sprintf("**%s**", truncate(strings.Join(current.Answers, ", "), 2000)),
					Color:       0xAA2222,
				}

				if len(current.Comment) > 0 {
					embed.Fields = []*discordgo.MessageEmbedField{
						&discordgo.MessageEmbedField{
							Name:   "Comment",
							Value:  truncate(current.Comment, 1024),
							Inline: false,
						}}
				}

				embedSend(s, quizChannel, embed)

				// Mark latest question as failed in quiz history
				quizHistory[len(quizHistory)-1] = "*" + quizHistory[len(quizHistory)-1]

				timeoutCount++
				if timeoutCount >= timeoutLimit {
					msgSend(s, quizChannel, "```Too many timeouts in a row reached, aborting quiz.```")
					break outer
				}
				break inner
			case msg := <-c:
				// Handle passing on question
				if msg.Content == ".." || msg.Content == "。。" {

					// Abort the question
					timeoutChan.Reset(0)

				} else if ts, okay := answerMap[k2h(strings.ToLower(msg.Content))]; okay {

					// Only count answers that are given within the window
					if ts.IsZero() {
						answerMap[k2h(strings.ToLower(msg.Content))] = time.Now()
						answersLeft--

						// Finish early if all answers given
						if answersLeft <= 0 {
							timeoutChan.Reset(waitTime)
						}
					} else if time.Since(ts) > waitTime {
						break
					}

					scoreKeeper[msg.Author.ID]++
					scoreKeeperAnswers[msg.Author.ID] = append(scoreKeeperAnswers[msg.Author.ID], msg.Content)

					// Reset timeouts since we're active
					timeoutCount = 0
				}
			}
		}

		if len(scoreKeeper) > 0 {

			winnerExists := false
			for player, score := range scoreKeeper {
				players[player] += minint(score, pointLimit)
				if players[player] >= winLimit {
					winnerExists = true
				}
			}

			var participants string
			for _, p := range ranking(scoreKeeper) {
				participants += fmt.Sprintf(
					"<@%s> +%d (%dp): %s\n",
					p.Name,
					minint(p.Score, pointLimit),
					players[p.Name],
					strings.Join(scoreKeeperAnswers[p.Name], ", "),
				)
			}

			embed := &discordgo.MessageEmbed{
				Type:        "rich",
				Title:       fmt.Sprintf(UNICODE_CHECK_MARK+" Correct: %s", questionTitle),
				Description: fmt.Sprintf("**%s**", truncate(strings.Join(current.Answers, ", "), 2000)),
				Color:       0x22AA22,
				Fields: []*discordgo.MessageEmbedField{
					&discordgo.MessageEmbedField{
						Name:   fmt.Sprintf("Scorers - %s to %d", quizname, winLimit),
						Value:  participants,
						Inline: false,
					}},
			}

			if len(current.Comment) > 0 {
				embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
					Name:   "Comment",
					Value:  truncate(current.Comment, 1024),
					Inline: false,
				})
			}

			embedSend(s, quizChannel, embed)

			if winnerExists {
				break outer
			}
		}

	}

	// Clean up
	killHandler()

	// Produce scoreboard
	fields := make([]*discordgo.MessageEmbedField, 0, 2)
	var winners string
	var participants string
	rankingList := ranking(players)

	for _, p := range rankingList {
		if p.Score >= winLimit && len(rankingList) > 0 && p.Score == rankingList[0].Score {
			winners += fmt.Sprintf("<@%s>: %d points\n", p.Name, p.Score)
		} else {
			participants += fmt.Sprintf("<@%s>: %d point(s)\n", p.Name, p.Score)
		}
	}

	if len(winners) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Winner",
			Value:  winners,
			Inline: false,
		})
	}

	if len(participants) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Participants",
			Value:  participants,
			Inline: false,
		})
	}

	// Sleep for a little breathing room
	time.Sleep(1 * time.Second)

	embed := &discordgo.MessageEmbed{
		Type:        "rich",
		Title:       "Final Quiz Scoreboard: " + quizname,
		Description: "-------------------------------",
		Color:       0x33FF33,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: truncate(strings.Join(quizHistory, "　"), 2000)},
	}

	embedSend(s, quizChannel, embed)

	stopQuiz(s, quizChannel)
}

// Run private gauntlet quiz
func runGauntlet(s *discordgo.Session, m *discordgo.MessageCreate, quizname, timeLimitGiven string) {

	quizChannel := m.ChannelID

	// Only react in private messages
	var retryErr error
	for i := 0; i < 3; i++ {
		var ch *discordgo.Channel
		ch, retryErr = s.State.Channel(quizChannel)
		if retryErr != nil {
			if strings.HasPrefix(retryErr.Error(), "HTTP 5") {
				// Wait and retry if Discord server related
				time.Sleep(250 * time.Millisecond)
				continue
			} else {
				break
			}
		} else if ch.Type&discordgo.ChannelTypeDM == 0 {
			// Not a private channel
			msgSend(s, quizChannel, fmt.Sprintf(UNICODE_NO_ENTRY_SIGN+" Game mode `%sgauntlet` is only for DM!", CMD_PREFIX))
			return
		}

		break
	}
	if retryErr != nil {
		log.Println("ERROR, With channel name check:", retryErr)
		return
	}

	// Mark the quiz as started
	if err := startQuiz(s, quizChannel); err != nil {
		// Quiz already running, nothing to do here
		return
	}

	timeout := 120 // seconds to run complete gauntlet

	quiz := LoadQuiz(quizname, true)
	if len(quiz.Deck) == 0 {
		msgSend(s, quizChannel, "Failed to find quiz: "+quizname)
		stopQuiz(s, quizChannel)
		return
	}

	// Parse provided time limit with sane defaults
	if i, err := strconv.Atoi(timeLimitGiven); err == nil {
		if i > 20 {
			timeout = 20 * 60
		} else if i > 0 {
			timeout = i * 60
		}
	}

	c := make(chan *discordgo.MessageCreate, 100)
	quitChan := make(chan struct{}, 100)

	killHandler := s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by self and bots
		if m.Author.ID == s.State.User.ID || m.Author.Bot {
			return
		}

		// Only react on current quiz channel
		if m.ChannelID != quizChannel {
			return
		}

		// Handle quiz aborts
		if strings.ToLower(strings.TrimSpace(m.Content)) == CMD_PREFIX+"stop" {
			quitChan <- struct{}{}
			return
		}

		// Relay the message to the quiz loop
		c <- m
	})

	msgSend(s, quizChannel, fmt.Sprintf("```Starting new %s quiz (%d questions) in 5 seconds:\n\"%s\"\nAnswer as many as you can within %d seconds.```", quizname, len(quiz.Deck), quiz.Description, timeout))

	var correct, total int
	var quizHistory []string

	// Breathing room to read start info
	time.Sleep(5 * time.Second)

	// Set start time and quiz timeout
	startTime := time.Now()
	timeoutChan := time.NewTimer(time.Duration(timeout) * time.Second)

outer:
	for len(quiz.Deck) > 0 {

		// Grab new word from the quiz
		var current Card
		current, quiz.Deck = quiz.Deck[len(quiz.Deck)-1], quiz.Deck[:len(quiz.Deck)-1]

		// Replace readings with hiragana-only version
		answers := make([]string, len(current.Answers))
		for i, ans := range current.Answers {
			answers[i] = k2h(ans)
		}

		// Send out quiz question
		if quiz.Type == "text" {
			msgSend(s, quizChannel, fmt.Sprintf("```\n%s```", current.Question))
		} else if quiz.Type == "url" {
			msgSend(s, quizChannel, current.Question)
		} else {
			imgSend(s, quizChannel, current.Question)
		}

		select {
		case <-quitChan:
			timeout = int(time.Since(startTime) / time.Second)
			break outer
		case <-timeoutChan.C:
			break outer
		case msg := <-c:
			// Increase total question count
			total++

			// Increase score if correct answer
			if hasString(answers, k2h(msg.Content)) {
				correct++
			} else {
				// Add wrong answer to quiz history
				if quiz.Type == "text" && len(current.Answers) > 0 {
					quizHistory = append(quizHistory, current.Answers[0])
				} else {
					quizHistory = append(quizHistory, current.Question)
				}
			}
		}
	}

	// Clean up
	killHandler()

	// Sleep for a little breathing room
	time.Sleep(1 * time.Second)

	var score float64
	if total > 0 {
		score = float64(correct*correct) / float64(total)
	}

	// Produce scoreboard
	embed := &discordgo.MessageEmbed{
		Type:        "rich",
		Title:       "Final Gauntlet Score: " + quizname,
		Description: fmt.Sprintf("%.2f points in %d seconds", score, timeout),
		Color:       0x33FF33,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Mistakes: " + truncate(strings.Join(quizHistory, "　"), 2000)},
	}

	embedSend(s, quizChannel, embed)

	stopQuiz(s, quizChannel)

	// Produce public scoreboard if no time limit specified
	if len(getStorage("output")) != 0 && timeLimitGiven == "" {

		embed := &discordgo.MessageEmbed{
			Type:        "rich",
			Title:       UNICODE_STOPWATCH + " New Gauntlet Score: " + quizname,
			Description: fmt.Sprintf("%s: %.2f points in %d seconds", m.Author.Mention(), score, timeout),
			Color:       0xFFAAAA,
		}

		embedSend(s, getStorage("output"), embed)
	}
}

// Scramble quiz
func runScramble(s *discordgo.Session, quizChannel string, difficulty string) {

	// Mark the quiz as started
	if err := startQuiz(s, quizChannel); err != nil {
		// Quiz already running, nothing to do here
		return
	}

	quizname := "Scramble"
	winLimit := 10                                                           // winner score
	timeout := 30                                                            // seconds to wait per round
	timeoutLimit := 5                                                        // count before aborting
	minLength := 3                                                           // default word length minimum
	maxLength := 7                                                           // default word length maximum
	pauseTime := time.Duration(Settings.Speed["quiz"][1]) * time.Millisecond // delay before next question

	// Set delay before closing round
	waitTime := time.Duration(Settings.Speed["quiz"][0]) * time.Millisecond

	// Parse provided winLimit with sane defaults
	if level, okay := Settings.Difficulty[difficulty]; okay {
		minLength, maxLength = level[0], level[1]
	}

	c := make(chan *discordgo.MessageCreate, 100)
	quitChan := make(chan struct{}, 100)

	killHandler := s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by self and bots
		if m.Author.ID == s.State.User.ID || m.Author.Bot {
			return
		}

		// Only react on current quiz channel
		if m.ChannelID != quizChannel {
			return
		}

		// Handle quiz aborts
		if strings.ToLower(strings.TrimSpace(m.Content)) == CMD_PREFIX+"stop" {
			quitChan <- struct{}{}
			return
		}

		// Relay the message to the quiz loop
		c <- m
	})

	// Create an index order, then shuffle it
	order := make([]int, len(Dictionary))
	for i := range order {
		order[i] = i
	}
	shuffle(order)

	msgSend(s, quizChannel, fmt.Sprintf("```Starting new %s quiz (%d questions) in %.f seconds:\n\"%s\"\nFirst to %d points wins.```", quizname, len(Dictionary), float64(pauseTime/time.Second), "Unscramble the English word", winLimit))

	var quizHistory []string
	players := make(map[string]int)
	var timeoutCount int

outer:
	for _, idx := range order {

		// Pick a group of scramble words from the Dictionary
		group := Dictionary[idx]

		// Grab a representative word to work with
		word := group[0]

		// Skip words that are too short/long
		if len(word) < minLength || len(word) > maxLength {
			continue outer
		}

		var question string

		// Attempt to shuffle thrice to get something random enough
		for i := 0; i < 3; i++ {
			shuffled := []rune(word)
			shuffle(shuffled)
			if !hasString(group, string(shuffled)) {
				question = string(shuffled)
				break
			}
		}

		// If we're still left with a proper word, give up and pick a new one
		if len(question) == 0 {
			continue outer
		}

		// Add word to quiz history
		quizHistory = append(quizHistory, word)

		// Round's score keeper
		scoreKeeper := make(map[string]int)

		// Give players time to breathe between rounds
		time.Sleep(pauseTime)

		// Drain premature "answers" from channel buffer
		for len(c) > 0 {
			<-c
		}

		// Send out quiz question
		imgSend(s, quizChannel, question)

		// Set timeout for no correct answers
		timeoutChan := time.NewTimer(time.Duration(timeout) * time.Second)

	inner:
		for {

			select {
			case <-quitChan:
				break outer
			case <-timeoutChan.C:
				if len(scoreKeeper) > 0 {
					break inner
				}

				embed := &discordgo.MessageEmbed{
					Type:        "rich",
					Title:       fmt.Sprintf(UNICODE_NO_ENTRY+" Timed out! %s", truncate(question, 100)),
					Description: fmt.Sprintf("**%s**", strings.Join(group, ", ")),
					Color:       0xAA2222,
				}

				embedSend(s, quizChannel, embed)

				// Mark latest question as failed in quiz history
				quizHistory[len(quizHistory)-1] = "*" + quizHistory[len(quizHistory)-1]

				timeoutCount++
				if timeoutCount >= timeoutLimit {
					msgSend(s, quizChannel, "```Too many timeouts in a row reached, aborting quiz.```")
					break outer
				}
				break inner
			case msg := <-c:
				if len(msg.Content) != len(word) {
					break
				}

				answer := strings.ToLower(msg.Content)

				// Check to see the answer is part of the valid set
				if !hasString(group, answer) {
					break
				}

				if len(scoreKeeper) == 0 {
					timeoutChan.Reset(waitTime)
				}

				// Make sure we don't add the same user again
				if _, exists := scoreKeeper[msg.Author.ID]; !exists {
					scoreKeeper[msg.Author.ID] = len(scoreKeeper) + 1
				}

				// Reset timeouts since we're active
				timeoutCount = 0
			}
		}

		if len(scoreKeeper) > 0 {

			winnerExists := false
			var fastest string
			var scorers []string
			for player, position := range scoreKeeper {
				players[player]++
				if position == 1 {
					fastest = fmt.Sprintf("<@%s> %dp", player, players[player])
				} else {
					scorers = append(scorers, fmt.Sprintf("<@%s> %dp", player, players[player]))
				}
				if players[player] >= winLimit {
					winnerExists = true
				}
			}

			scorers = append([]string{fastest}, scorers...)

			embed := &discordgo.MessageEmbed{
				Type:        "rich",
				Title:       fmt.Sprintf(UNICODE_CHECK_MARK+" Correct: %s", truncate(question, 100)),
				Description: fmt.Sprintf("**%s**", truncate(strings.Join(group, ", "), 2000)),
				Color:       0x22AA22,
				Fields: []*discordgo.MessageEmbedField{
					&discordgo.MessageEmbedField{
						Name:   fmt.Sprintf("Scorers - %s to %d", quizname, winLimit),
						Value:  strings.Join(scorers, ", "),
						Inline: false,
					}},
			}

			embedSend(s, quizChannel, embed)

			if winnerExists {
				break outer
			}
		}

	}

	// Clean up
	killHandler()

	// Produce scoreboard
	fields := make([]*discordgo.MessageEmbedField, 0, 2)
	var winners string
	var participants string

	for _, p := range ranking(players) {
		if p.Score >= winLimit {
			winners += fmt.Sprintf("<@%s>: %d points\n", p.Name, p.Score)
		} else {
			participants += fmt.Sprintf("<@%s>: %d point(s)\n", p.Name, p.Score)
		}
	}

	if len(winners) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Winner",
			Value:  winners,
			Inline: false,
		})
	}

	if len(participants) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Participants",
			Value:  participants,
			Inline: false,
		})
	}

	// Sleep for a little breathing room
	time.Sleep(1 * time.Second)

	embed := &discordgo.MessageEmbed{
		Type:        "rich",
		Title:       "Final Quiz Scoreboard: " + quizname,
		Description: "-------------------------------",
		Color:       0x33FF33,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: strings.Join(quizHistory, "　")},
	}

	embedSend(s, quizChannel, embed)

	stopQuiz(s, quizChannel)
}

// Run sequential kanji quiz loop in given channel
func runQuizSequential(s *discordgo.Session, quizChannel string, quizname string, startIndex string, waitTimeGiven int, pauseTimeGiven int) {

	// Mark the quiz as started
	if err := startQuiz(s, quizChannel); err != nil {
		// Quiz already running, nothing to do here
		return
	}

	timeout := 20                                                 // seconds to wait per round
	timeoutLimit := 5                                             // count before aborting
	pauseTime := time.Duration(pauseTimeGiven) * time.Millisecond // delay before next question

	// Set delay before closing round
	waitTime := time.Duration(waitTimeGiven) * time.Millisecond

	var quiz Quiz
	if quizname == "review" {
		quiz = getReview(quizChannel)
	} else {
		quiz = LoadQuiz(quizname, false)
	}
	if len(quiz.Deck) == 0 {
		msgSend(s, quizChannel, "Failed to find valid quiz: "+quizname)
		stopQuiz(s, quizChannel)
		return
	}

	// Parse provided start index
	idx, _ := strconv.Atoi(startIndex[:strings.Index(startIndex, "-")])
	if idx > len(quiz.Deck) {
		idx = len(quiz.Deck)
	}

	// Flash forward deck up to given index
	quiz.Deck = quiz.Deck[idx:]

	// Figure out maximum possible points
	pointLimit := len(quiz.Deck)

	// Replace default timeout with custom if specified
	if quiz.Timeout > 0 {
		timeout = quiz.Timeout
	}

	c := make(chan *discordgo.MessageCreate, 100)
	quitChan := make(chan struct{}, 100)

	killHandler := s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by self and bots
		if m.Author.ID == s.State.User.ID || m.Author.Bot {
			return
		}

		// Only react on current quiz channel
		if m.ChannelID != quizChannel {
			return
		}

		// Handle quiz aborts
		if strings.ToLower(strings.TrimSpace(m.Content)) == CMD_PREFIX+"stop" {
			quitChan <- struct{}{}
			return
		}

		// Relay the message to the quiz loop
		c <- m
	})

	msgSend(s, quizChannel, fmt.Sprintf("```Starting new %s quiz (%d questions) in %.f seconds:\n\"%s\"\nType %sstop to give up.```", quizname, len(quiz.Deck), float64(pauseTime/time.Second), quiz.Description, CMD_PREFIX))

	var quizHistory []string
	var failed []Card
	var questionTitle string
	players := make(map[string]int)
	var timeoutCount int

outer:
	for len(quiz.Deck) > 0 {
		time.Sleep(pauseTime)

		// Grab new word from the quiz
		var current Card
		current, quiz.Deck = quiz.Deck[0], quiz.Deck[1:]

		// Replace readings with hiragana-only version
		answers := make([]string, len(current.Answers))
		for i, ans := range current.Answers {
			answers[i] = k2h(ans)
		}

		// Add word to quiz history
		if (quiz.Type == "text" || quiz.Type == "url") && len(current.Answers) > 0 {
			quizHistory = append(quizHistory, current.Answers[0])
			questionTitle = ""
		} else {
			quizHistory = append(quizHistory, current.Question)
			questionTitle = truncate(current.Question, 100)
		}

		// Round's score keeper
		scoreKeeper := make(map[string]int)

		// Drain premature "answers" from channel buffer
		for len(c) > 0 {
			<-c
		}

		// Send out quiz question
		if quiz.Type == "text" {
			msgSend(s, quizChannel, fmt.Sprintf("```\n%s```", current.Question))
		} else if quiz.Type == "url" {
			msgSend(s, quizChannel, current.Question)
		} else {
			imgSend(s, quizChannel, current.Question)
		}

		// Set timeout for no correct answers
		timeoutChan := time.NewTimer(time.Duration(timeout) * time.Second)

	inner:
		for {

			select {
			case <-quitChan:
				// Quit order received, but store remaining questions for reviews
				if quizname == "review" {
					// Did this question get answered
					if len(scoreKeeper) == 0 {
						// Store question for later review deck
						failed = append(failed, current)
					}

					// Store unused review questions
					failed = append(failed, quiz.Deck...)
				}
				break outer
			case <-timeoutChan.C:
				if len(scoreKeeper) > 0 {
					break inner
				}

				embed := &discordgo.MessageEmbed{
					Type:        "rich",
					Title:       fmt.Sprintf(UNICODE_NO_ENTRY+" Timed out! %s", questionTitle),
					Description: fmt.Sprintf("**%s**", truncate(strings.Join(current.Answers, ", "), 2000)),
					Color:       0xAA2222,
				}

				if len(current.Comment) > 0 {
					embed.Fields = []*discordgo.MessageEmbedField{
						&discordgo.MessageEmbedField{
							Name:   "Comment",
							Value:  truncate(current.Comment, 1024),
							Inline: false,
						}}
				}

				embedSend(s, quizChannel, embed)

				// Store question for later review deck
				failed = append(failed, current)

				// Mark latest question as failed in quiz history as well
				quizHistory[len(quizHistory)-1] = "*" + quizHistory[len(quizHistory)-1]

				timeoutCount++
				if timeoutCount >= timeoutLimit {
					msgSend(s, quizChannel, "```Too many timeouts in a row reached, aborting quiz.```")

					if quizname == "review" {
						// Store unused review questions
						failed = append(failed, quiz.Deck...)
					}

					break outer
				}
				break inner
			case msg := <-c:
				// Handle passing on question
				if msg.Content == ".." || msg.Content == "。。" {

					// Abort the question
					timeoutChan.Reset(0)

				} else if hasString(answers, k2h(msg.Content)) {
					if len(scoreKeeper) == 0 {
						timeoutChan.Reset(waitTime)
					}

					// Make sure we don't add the same user again
					if _, exists := scoreKeeper[msg.Author.ID]; !exists {
						scoreKeeper[msg.Author.ID] = len(scoreKeeper) + 1
					}

					// Reset timeouts since we're active
					timeoutCount = 0
				}
			}
		}

		if len(scoreKeeper) > 0 {

			var fastest string
			var scorers []string
			for player, position := range scoreKeeper {
				players[player]++
				if position == 1 {
					fastest = fmt.Sprintf("<@%s> %dp", player, players[player])
				} else {
					scorers = append(scorers, fmt.Sprintf("<@%s> %dp", player, players[player]))
				}
			}

			scorers = append([]string{fastest}, scorers...)

			embed := &discordgo.MessageEmbed{
				Type:        "rich",
				Title:       fmt.Sprintf(UNICODE_CHECK_MARK+" #%d Correct: %s", idx, questionTitle),
				Description: fmt.Sprintf("**%s**", truncate(strings.Join(current.Answers, ", "), 2000)),
				Color:       0x22AA22,
				Fields: []*discordgo.MessageEmbedField{
					&discordgo.MessageEmbedField{
						Name:   fmt.Sprintf("Scorers - %s to %d", quizname, pointLimit),
						Value:  strings.Join(scorers, ", "),
						Inline: false,
					}},
			}

			if len(current.Comment) > 0 {
				embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
					Name:   "Comment",
					Value:  truncate(current.Comment, 1024),
					Inline: false,
				})
			}

			embedSend(s, quizChannel, embed)
		}

		// Increase question index
		idx++
	}

	// Clean up
	killHandler()

	// Produce scoreboard
	fields := make([]*discordgo.MessageEmbedField, 0, 2)
	var participants string

	for _, p := range ranking(players) {
		participants += fmt.Sprintf("<@%s>: %d point(s)\n", p.Name, p.Score)
	}

	if len(participants) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Participants",
			Value:  participants,
			Inline: false,
		})
	}

	if len(quiz.Deck) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Resuming",
			Value:  fmt.Sprintf("Try `%squiz %s %d-` to continue from the last question\n", CMD_PREFIX, quizname, idx),
			Inline: false,
		})
	}

	if len(failed) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Note",
			Value:  fmt.Sprintf("Try `%squiz review` to replay the %d failed question(s)\n", CMD_PREFIX, len(failed)),
			Inline: false,
		})
	}

	// Sleep for a little breathing room
	time.Sleep(1 * time.Second)

	embed := &discordgo.MessageEmbed{
		Type:        "rich",
		Title:       "Final Quiz Scoreboard: " + quizname,
		Description: "-------------------------------",
		Color:       0x33FF33,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: truncate(strings.Join(quizHistory, "　"), 2000)},
	}

	embedSend(s, quizChannel, embed)

	// Store review questions in memory
	quiz.Deck = failed
	putReview(quizChannel, copyQuiz(quiz))

	stopQuiz(s, quizChannel)
}
