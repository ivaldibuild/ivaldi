package authhttp

import (
	"html/template"
	"net/http"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// The sign-in pages are deliberately plain: no scripts, and only inline
// styles, which the pages' Content-Security-Policy allows and nothing else.
var pages = template.Must(template.New("layout").Parse(`
{{define "layout"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Ivaldi</title>
<style>
  :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; }
  main { width: min(24rem, 100% - 2rem); }
  h1 { font-size: 1.4rem; }
  label { display: block; margin: 0.8rem 0 0.3rem; }
  input, button, .button { box-sizing: border-box; width: 100%; padding: 0.6rem; font: inherit; }
  button, .button { margin-top: 1rem; cursor: pointer; display: block; text-align: center; }
  .error { padding: 0.6rem; border: 1px solid #c33; border-radius: 4px; }
  .or { text-align: center; margin: 1.2rem 0 0; opacity: 0.7; }
</style>
</head>
<body><main>{{template "content" .}}</main></body>
</html>{{end}}

{{define "login"}}{{template "layout" .}}{{end}}
{{define "organizations"}}{{template "layout" .}}{{end}}
{{define "message"}}{{template "layout" .}}{{end}}
`))

var loginContent = template.Must(template.Must(pages.Clone()).Parse(`{{define "content"}}
<h1>Sign in to Ivaldi</h1>
{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
{{if .Builtin}}
<form method="post" action="{{.LoginPath}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="return_to" value="{{.ReturnTo}}">
  <label for="email">Email</label>
  <input id="email" name="email" type="email" autocomplete="username" required value="{{.Email}}">
  <label for="password">Password</label>
  <input id="password" name="password" type="password" autocomplete="current-password" required>
  <button type="submit">Sign in</button>
</form>
{{end}}
{{if and .Builtin .Providers}}<p class="or">or</p>{{end}}
{{range .Providers}}<a class="button" href="{{.StartURL}}">Continue with {{.Name}}</a>{{end}}
{{if .SignUp}}<p class="or">New to Ivaldi? Continuing with one of these creates your account.</p>{{end}}
{{if and (not .Builtin) (not .Providers)}}<p>No way to sign in is configured.</p>{{end}}
{{end}}`))

var organizationsContent = template.Must(template.Must(pages.Clone()).Parse(`{{define "content"}}
<h1>Choose an organization</h1>
{{range .Memberships}}
<form method="post" action="{{$.OrganizationsPath}}">
  <input type="hidden" name="csrf" value="{{$.CSRF}}">
  <input type="hidden" name="return_to" value="{{$.ReturnTo}}">
  <input type="hidden" name="organization" value="{{.OrganizationID}}">
  <button type="submit">{{.OrganizationName}}</button>
</form>
{{end}}
{{end}}`))

var messageContent = mustClone(`{{define "content"}}
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
{{end}}`)

// mustClone gives the layout a page's content.
func mustClone(content string) *template.Template {
	return template.Must(template.Must(pages.Clone()).Parse(content))
}

// ProviderLink is a sign-in button for an identity provider.
type ProviderLink struct {
	Name     string
	StartURL string
}

type loginData struct {
	Title, Error, CSRF, ReturnTo, Email, LoginPath string
	Builtin, SignUp                                bool
	Providers                                      []ProviderLink
}

type organizationsData struct {
	Title, CSRF, ReturnTo, OrganizationsPath string
	Memberships                              []iam.Membership
}

type messageData struct {
	Title, Message string
}

func (h *handlers) render(w http.ResponseWriter, status int, t *template.Template, name string, data any) {
	pageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error("render sign-in page", logging.F("error", err))
	}
}

func (h *handlers) renderError(w http.ResponseWriter, status int, message string) {
	h.render(w, status, messageContent, "message", messageData{Title: "Something went wrong", Message: message})
}

func (h *handlers) renderMessage(w http.ResponseWriter, title, message string) {
	h.render(w, http.StatusOK, messageContent, "message", messageData{Title: title, Message: message})
}
