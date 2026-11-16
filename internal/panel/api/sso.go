package api

// ssoProvider is a single sign-on option shown on the sign-in page.
type ssoProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *App) ssoList() []ssoProvider { return []ssoProvider{} }
