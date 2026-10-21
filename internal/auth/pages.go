package auth

// LoginPage is the data of login.html.
type LoginPage struct {
	Brand      string
	Next       string
	Error      string
	LocalLogin bool
	Providers  []LoginProviderLink
}

// LoginProviderLink is one rendered SSO button.
type LoginProviderLink struct {
	Name string
	URL  string
}

func (LoginPage) Title() string { return "Log in" }

// DeniedPage is the data of denied.html. It holds only the claims of the user
// who just logged in.
type DeniedPage struct {
	Subject  string
	Username string
	Name     string
	Email    string
	Claim    string
	Values   []string
}

func (DeniedPage) Title() string { return "No access" }
