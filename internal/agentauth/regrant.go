package agentauth

// RegrantCommand returns the commands a human runs to replace c with a client
// that also holds scopes, keeping its chats and rate. Expiring grants require
// an explicit human expiry decision instead of a copyable permanent replacement.
// It never includes secrets.
func RegrantCommand(c *Client, scopes ...string) string {
	next := cloneClient(*c)
	for _, s := range scopes {
		if !next.HasScope(s) {
			next.Scopes = append(next.Scopes, s)
		}
	}
	return regrantCommand(&next, "", "", 0)
}

// Dir returns the data directory this authorizer reads.
func (a *Authorizer) Dir() string { return a.dir }
