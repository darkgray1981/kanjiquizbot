# kanjiquizbot
Kanji Quiz Bot for Discord written in Go

Quiz data is stored in .json files inside the quizzes folder. The format used is:
```
{
	"description": "A test deck",
	"deck": [
		{ "question": "未来",	"answers": [ "みらい" ] },
		{ "question": "On-yomi for 回",	"answers": [ "え", "かい" ] }
	]
}
```

Use this URL to invite your bot to a server:  
https://discordapp.com/oauth2/authorize?scope=bot&client_id=BOT_CLIENT_ID_GOES_HERE  
after creating an app with the [Discord API](https://discordapp.com/developers/docs/intro).

Uses the [DiscordGo](https://github.com/bwmarrin/discordgo) project for API bindings and whatnot, and [Golang Freetype](https://github.com/golang/freetype) to draw fonts on an image.

# Command List

*Games*  
`kq!help` - shows help message.  
`kq!quiz <deck> [optional max score]` - runs a quiz with the specified deck until a player reaches optional max score.  
`kq!stop` - ends a running quiz immediately.  
`kq!list` - shows a full list of loaded quizzes.  
`kq!mad/fast/quiz/mild/slow <deck>` - for 0/1/2/3/5 second answer windows instead.  
`kq!flash <deck>` - for no pause between questions.  
`kq!gauntlet <deck>` - runs a kanji time trial in Direct Message.  
`kq!scramble [easy/normal/hard/insane]` - runs an English Word Scramble quiz with varying word length limits.

*Utilities*  
`kq!k <kanji>` - displays kanji information.  
`kq!f <word>` - shows usage frequency statistics for given Japanese word.  
`kq!p <word>` - shows pitch accent information for given word.  
`kq!c <X currency in Y currency>` - converts between given currencies.  
`kq!time` - shows current time in UTC.  
`kq!ping` - measures the bot's latency to the server.  
`kq!draw <text>` - creates an image with given text drawn on it.

*Administration*  
`kq!uptime` - shows how long the bot has been running.  
`kq!ongoing` - shows currently active quiz sessions.  
`kq!output` - locks Gauntlet score announcements to current channel.  
`kq!reload` - reloads the quiz list file for live quizzing adjustments.  
