package identity

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Claims are what an access token says about its holder. This type owns the
// tokens' wire format, so the control plane that issues them and every
// deployable that verifies them agree on it.
type Claims struct {
	// Issuer is the authorization server, the control plane's external URL.
	Issuer string
	// Subject is the principal's ID.
	Subject string
	// Audience is the resource servers the token is for.
	Audience  []string
	IssuedAt  time.Time
	NotBefore time.Time
	ExpiresAt time.Time
	// ID is unique per token.
	ID             string
	OrganizationID string
	// ClientID is the OAuth client the token was issued to.
	ClientID string
	Scopes   []string
	Kind     ActorKind
	// SessionID is set for an agent Session's token.
	SessionID string
}

// Actor returns who the token speaks for.
func (c *Claims) Actor() Actor {
	return Actor{PrincipalID: c.Subject, Kind: c.Kind, SessionID: c.SessionID}
}

// HasAudience reports whether the token is for the resource server.
func (c *Claims) HasAudience(aud string) bool { return slices.Contains(c.Audience, aud) }

// wireClaims is the JSON form: RFC 7519's registered claims, RFC 9068's
// client_id and scope, and Ivaldi's org, kind, and sid.
type wireClaims struct {
	Issuer         string   `json:"iss"`
	Subject        string   `json:"sub"`
	Audience       audience `json:"aud"`
	ExpiresAt      int64    `json:"exp"`
	IssuedAt       int64    `json:"iat"`
	NotBefore      int64    `json:"nbf,omitempty"`
	ID             string   `json:"jti"`
	OrganizationID string   `json:"org,omitempty"`
	ClientID       string   `json:"client_id,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	Kind           string   `json:"kind"`
	SessionID      string   `json:"sid,omitempty"`
}

func (c Claims) MarshalJSON() ([]byte, error) {
	w := wireClaims{
		Issuer:         c.Issuer,
		Subject:        c.Subject,
		Audience:       c.Audience,
		ExpiresAt:      c.ExpiresAt.Unix(),
		IssuedAt:       c.IssuedAt.Unix(),
		ID:             c.ID,
		OrganizationID: c.OrganizationID,
		ClientID:       c.ClientID,
		Scope:          strings.Join(c.Scopes, " "),
		Kind:           string(c.Kind),
		SessionID:      c.SessionID,
	}
	if !c.NotBefore.IsZero() {
		w.NotBefore = c.NotBefore.Unix()
	}
	return json.Marshal(w)
}

func (c *Claims) UnmarshalJSON(b []byte) error {
	var w wireClaims
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*c = Claims{
		Issuer:         w.Issuer,
		Subject:        w.Subject,
		Audience:       w.Audience,
		ExpiresAt:      time.Unix(w.ExpiresAt, 0).UTC(),
		IssuedAt:       time.Unix(w.IssuedAt, 0).UTC(),
		ID:             w.ID,
		OrganizationID: w.OrganizationID,
		ClientID:       w.ClientID,
		Scopes:         strings.Fields(w.Scope),
		Kind:           ActorKind(w.Kind),
		SessionID:      w.SessionID,
	}
	if w.NotBefore != 0 {
		c.NotBefore = time.Unix(w.NotBefore, 0).UTC()
	}
	return nil
}

// audience is aud, which RFC 7519 allows as one string or an array.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("aud must be a string or an array of strings")
	}
	*a = many
	return nil
}
