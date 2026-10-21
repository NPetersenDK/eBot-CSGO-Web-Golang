// Package store is the only place that talks SQL. It owns the mapping between
// eBot's shared MySQL schema (French column names included) and the English
// types below. Nothing outside this package should know a column name.
package store

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrMatchEnabled is returned when a running match is edited or deleted.
	ErrMatchEnabled = errors.New("match is enabled")
	// ErrServerBusy is returned when a server already runs an enabled match.
	ErrServerBusy = errors.New("server is in use")
	// ErrDuplicate is returned for a unique key violation (username, email).
	ErrDuplicate = errors.New("already exists")
	// ErrInvalid marks input the store refuses. The error text is safe to show to the user.
	ErrInvalid = errors.New("invalid input")
)

type invalidError string

func (e invalidError) Error() string        { return string(e) }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(msg string) error { return invalidError(msg) }

// Match statuses, as stored in matchs.status.
const (
	StatusNotStarted = 0
	StatusStarting   = 1
	StatusWUKnife    = 2
	StatusKnife      = 3
	StatusEndKnife   = 4
	StatusWU1Side    = 5
	StatusFirstSide  = 6
	StatusWU2Side    = 7
	StatusSecondSide = 8
	StatusWUOT1      = 9
	StatusOTFirst    = 10
	StatusWUOT2      = 11
	StatusOTSecond   = 12
	StatusEndMatch   = 13
	StatusArchived   = 14
)

var statusLabels = [...]string{
	"Not started", "Starting", "Warmup knife", "Knife round", "End of knife round",
	"Warmup first side", "First side", "Warmup second side", "Second side",
	"Warmup overtime first side", "Overtime first side",
	"Warmup overtime second side", "Overtime second side",
	"Finished", "Archived",
}

// StatusLabel is the display text for a match status.
func StatusLabel(status int) string {
	if status < 0 || status >= len(statusLabels) {
		return "Unknown"
	}
	return statusLabels[status]
}

// StatusClass is the badge modifier for a status: "muted", "ok", "warn" or "err".
func StatusClass(status int) string {
	switch {
	case status == StatusNotStarted, status == StatusArchived:
		return "muted"
	case status == StatusEndMatch:
		return "ok"
	case status == StatusStarting:
		return "warn"
	default:
		return "ok"
	}
}

// IsRunning is true while the bot owns the match (status 1 to 12).
func IsRunning(status int) bool { return status >= StatusStarting && status < StatusEndMatch }

type Page struct {
	Page    int
	PerPage int
	Total   int
}

func (p Page) Pages() int {
	if p.PerPage <= 0 || p.Total <= 0 {
		return 1
	}
	return (p.Total + p.PerPage - 1) / p.PerPage
}
func (p Page) HasPrev() bool { return p.Page > 1 }
func (p Page) HasNext() bool { return p.Page < p.Pages() }
func (p Page) Prev() int     { return p.Page - 1 }
func (p Page) Next() int     { return p.Page + 1 }

type MatchFilter struct {
	// Archived selects status 14. Otherwise status 0 to 13.
	Archived bool
	SeasonID int64
	TeamID   int64
	// Search matches team names.
	Search  string
	Page    int
	PerPage int
}

type Match struct {
	ID       int64
	ServerID *int64
	SeasonID *int64
	// IP is the "host:port" the match runs on. The bot keys the auth key on it.
	IP string

	TeamAID   *int64
	TeamBID   *int64
	TeamAName string
	TeamBName string
	TeamAFlag string
	TeamBFlag string

	Status   int
	IsPaused bool
	ScoreA   int
	ScoreB   int

	MaxRound           int
	Rules              string
	OvertimeStartMoney int
	OvertimeMaxRound   int

	ConfigFullScore          bool
	ConfigOT                 bool
	ConfigStreamer           int
	ConfigKnifeRound         bool
	ConfigSwitchAuto         bool
	ConfigAutoChangePassword bool
	ConfigPassword           string
	ConfigHeatmap            bool
	ConfigAuthKey            string

	Enable           bool
	MapSelectionMode string
	IngameEnable     bool
	ForceZoomMatch   bool
	IdentifierID     string
	StartDate        *time.Time
	AutoStart        bool
	// AutoStartTime is minutes before StartDate.
	AutoStartTime int

	CurrentMapID *int64
	// CurrentMap is filled by GetMatch and ListMatches.
	CurrentMap *Map
	// ServerHostname is filled by GetMatch and ListMatches when ServerID is set.
	ServerHostname string
	// TVIP is the GOTV address of the server, filled like ServerHostname.
	TVIP string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// MatchInput is the editable part of a match, shared by create and update.
type MatchInput struct {
	ServerID *int64
	SeasonID *int64

	TeamAID   *int64
	TeamBID   *int64
	TeamAName string
	TeamBName string
	TeamAFlag string
	TeamBFlag string

	MaxRound           int
	Rules              string
	OvertimeStartMoney int
	OvertimeMaxRound   int

	ConfigFullScore          bool
	ConfigOT                 bool
	ConfigStreamer           int
	ConfigKnifeRound         bool
	ConfigSwitchAuto         bool
	ConfigAutoChangePassword bool
	ConfigPassword           string
	ConfigHeatmap            bool

	MapSelectionMode string
	IngameEnable     bool
	ForceZoomMatch   bool
	IdentifierID     string
	StartDate        *time.Time
	AutoStart        bool
	AutoStartTime    int

	// Maps has exactly one entry: the current map ("tba" when the map is chosen
	// in game). The bot creates the other maps of a series itself. Update
	// renames the current map and leaves any other map alone.
	Maps []MapInput
	// Side is the starting side of the current map, "ct" or "t". Create picks
	// one at random when it is empty.
	Side string
}

type MapInput struct {
	Name string
	// PickedBy is "default", "team1" or "team2".
	PickedBy string
}

type Map struct {
	ID      int64
	MatchID int64
	Name    string
	Score1  int
	Score2  int
	// CurrentSide is "ct" or "t".
	CurrentSide  string
	Status       int
	PickedBy     string
	Overtimes    int
	IdentifierID string
	// TVRecordFile is the demo file name without the .dem.zip suffix.
	TVRecordFile string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MapScore struct {
	ID          int64
	MapID       int64
	Type        string // "normal" or "ot"
	Score1Side1 int
	Score1Side2 int
	Score2Side1 int
	Score2Side2 int
}

type MapScoreInput struct {
	Score1Side1 int
	Score1Side2 int
	Score2Side1 int
	Score2Side2 int
}

type Player struct {
	ID          int64
	MatchID     int64
	MapID       int64
	Key         string
	Team        string // "a", "b" or "other"
	IP          string
	SteamID     string
	FirstSide   string
	CurrentSide string
	Nickname    string
	Kills       int
	Assists     int
	Deaths      int
	Points      int
	Headshots   int
	Defuses     int
	Bombs       int
	TeamKills   int
	Rounds1K    int
	Rounds2K    int
	Rounds3K    int
	Rounds4K    int
	Rounds5K    int
	Clutch1v1   int
	Clutch1v2   int
	Clutch1v3   int
	Clutch1v4   int
	Clutch1v5   int
	FirstKills  int
	PlusKills   int
}

type RoundSummary struct {
	ID             int64
	MatchID        int64
	MapID          int64
	RoundID        int
	BombPlanted    bool
	BombDefused    bool
	BombExploded   bool
	WinType        string
	TeamWin        string // "a" or "b"
	CTWin          bool
	TWin           bool
	ScoreA         int
	ScoreB         int
	BestKillerID   *int64
	BestKillerNb   int
	BestKillerFK   bool
	BackupFileName string
}

type Server struct {
	ID        int64
	IP        string
	RCON      string
	Hostname  string
	TVIP      string
	CreatedAt time.Time
	UpdatedAt time.Time
	// InUse is filled by ListServers: an enabled match runs on it.
	InUse bool
}

// ServerCreate describes one or many servers. Mode is "single" (Host:Port),
// "range" (Host with ports Port through PortEnd) or "hostname" (Host as a DNS
// name with Port). See servers/actions.class.php in the PHP app.
type ServerCreate struct {
	Mode     string
	Host     string
	Port     int
	PortEnd  int
	RCON     string
	Hostname string
	TVIP     string
}

type ServerInput struct {
	IP       string
	RCON     string
	Hostname string
	TVIP     string
}

type Team struct {
	ID        int64
	Name      string
	ShortName string
	Flag      string
	Link      string
	CreatedAt time.Time
	UpdatedAt time.Time
	// SeasonIDs is filled by GetTeam.
	SeasonIDs []int64
}

type TeamInput struct {
	Name      string
	ShortName string
	Flag      string
	Link      string
	SeasonIDs []int64
}

type Season struct {
	ID        int64
	Name      string
	Event     string
	Start     string
	End       string
	Link      string
	Logo      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SeasonInput struct {
	Name   string
	Event  string
	Start  string
	End    string
	Link   string
	Logo   string
	Active bool
}

type Ad struct {
	ID        int64
	SeasonID  int64
	Season    string
	Message   string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AdInput struct {
	SeasonID int64
	Message  string
	Active   bool
}

type User struct {
	ID           int64
	FirstName    string
	LastName     string
	Email        string
	Username     string
	IsActive     bool
	IsSuperAdmin bool
	LastLogin    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserInput is used for create and update. An empty Password on update keeps
// the current one.
type UserInput struct {
	FirstName    string
	LastName     string
	Email        string
	Username     string
	Password     string
	IsActive     bool
	IsSuperAdmin bool
}

// GlobalStats are the public numbers on the statistics page.
type GlobalStats struct {
	Kills      int64
	Headshots  int64
	InProgress int
	NotStarted int
	// Finished counts finished and archived matches.
	Finished int
	Servers  int
}

// HeadshotRate is the share of kills that were headshots, in percent. It is
// false when there are no kills.
func (g GlobalStats) HeadshotRate() (float64, bool) {
	if g.Kills <= 0 {
		return 0, false
	}
	return float64(g.Headshots) * 100 / float64(g.Kills), true
}

// HeadshotPercent is HeadshotRate as text for pages.
func (g GlobalStats) HeadshotPercent() string {
	rate, ok := g.HeadshotRate()
	if !ok {
		return "not available"
	}
	return fmt.Sprintf("%.1f%%", rate)
}

func (g GlobalStats) Matches() int { return g.InProgress + g.NotStarted + g.Finished }

type Counts struct {
	Matches  int
	Running  int
	Archived int
	Servers  int
	Teams    int
	Seasons  int
	Users    int
}
