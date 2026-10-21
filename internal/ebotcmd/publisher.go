package ebotcmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Admin commands the bot understands (see eBot/Application/Application.php).
const (
	// ActionStop is the panel's "Stop with Restart": RCON mp_restartgame,
	// enable=0, and the bot releases the server. status is left alone, so the
	// match can be reset and started again.
	ActionStop = "stop"
	// ActionStopNoRestart is the panel's plain "Stop".
	ActionStopNoRestart = "stopNoRs"
)

// Publisher sends admin commands to the eBot bot. It writes onto the same Redis
// list the panel's websocket bridge uses (websocket_server.mjs), so the bot
// cannot tell the difference - no socket.io hop needed.
type Publisher struct {
	rdb  *redis.Client
	list string
}

func NewPublisher(addr, username, password, list string) *Publisher {
	return &Publisher{
		rdb:  redis.NewClient(&redis.Options{Addr: addr, Username: username, Password: password}),
		list: list,
	}
}

func (p *Publisher) Close() error { return p.rdb.Close() }

func (p *Publisher) Ping(ctx context.Context) error { return p.rdb.Ping(ctx).Err() }

// Actions is the whitelist of commands the web panel may send. The bot
// handles each in eBot/Application/Application.php.
var Actions = map[string]bool{
	"stop":           true,
	"stopNoRs":       true,
	"executeCommand": true,
	"passknife":      true,
	"forceknife":     true,
	"forceknifeend":  true,
	"forcestart":     true,
	"stopback":       true,
	"pauseunpause":   true,
	"fixsides":       true,
	"streamerready":  true,
	"goBackRounds":   true,
	"skipmap":        true,
}

// ValidAction reports whether action is a command the bot understands.
func ValidAction(action string) bool { return Actions[action] }

// Send delivers one admin command for a match. serverIP is "host:port" and must
// be the address the bot registered the match under, it keys the authkey
// lookup on the bot side. authkey is the match's config_authkey. arg is the
// optional trailing argument (the RCON command for executeCommand, the round
// for goBackRounds).
func (p *Publisher) Send(ctx context.Context, matchID int64, action, arg, serverIP, authkey string) error {
	if !ValidAction(action) {
		return fmt.Errorf("unknown action %q", action)
	}
	plaintext := fmt.Sprintf("%d %s %s", matchID, action, serverIP)
	if arg != "" {
		plaintext += " " + arg
	}
	ciphertext, err := Encrypt(plaintext, authkey)
	if err != nil {
		return fmt.Errorf("encrypt command: %w", err)
	}
	msg, err := json.Marshal([]string{ciphertext, serverIP})
	if err != nil {
		return err
	}
	return p.Push(ctx, string(msg))
}

// Push writes a raw message onto the bot's inbound list. The bot LPOPs, so
// this is a stack by design.
func (p *Publisher) Push(ctx context.Context, raw string) error {
	return p.rdb.LPush(ctx, p.list, raw).Err()
}
