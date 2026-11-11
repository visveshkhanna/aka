package proxy

import (
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
)

type pageKind int

const (
	pageNotFound pageKind = iota
	pageStarting
	pageRestarting
	pageBadGateway
	pageLoop
	pageMissingHost
)

type pageRoute struct {
	Hostname, URL, Detail string
}

type pageData struct {
	Title, Heading, Message, Host, Tool, Hint string
	Refresh                                   bool
	Routes                                    []pageRoute
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="1">{{end}}
<title>{{.Title}}</title>
<style>
:root{--bg:#fafafa;--fg:#111;--muted:#666;--card:#fff;--border:#e5e5e5;--accent:#2563eb}
@media (prefers-color-scheme:dark){:root{--bg:#111;--fg:#eee;--muted:#999;--card:#1a1a1a;--border:#2a2a2a;--accent:#60a5fa}}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,sans-serif}
main{max-width:640px;margin:12vh auto;padding:0 16px}
h1{font-size:20px;margin:0 0 8px}
p{color:var(--muted);margin:0 0 16px}
code{font:13px ui-monospace,SFMono-Regular,Menlo,monospace;background:var(--card);border:1px solid var(--border);border-radius:4px;padding:1px 5px}
ul{list-style:none;padding:0;margin:16px 0;border:1px solid var(--border);border-radius:8px;background:var(--card)}
li{padding:10px 14px;border-top:1px solid var(--border);display:flex;justify-content:space-between;gap:12px}
li:first-child{border-top:0}
a{color:var(--accent);text-decoration:none}
small{color:var(--muted)}
footer{margin-top:32px;color:var(--muted);font-size:12px}
</style></head><body><main>
<h1>{{.Heading}}</h1>
<p>{{.Message}}</p>
{{if .Hint}}<p>{{.Hint}}</p>{{end}}
{{if .Routes}}<ul>{{range .Routes}}<li><a href="{{.URL}}">{{.Hostname}}</a><small>{{.Detail}}</small></li>{{end}}</ul>{{end}}
<footer>{{.Tool}} proxy</footer>
</main></body></html>`))

// sendError writes a proxy-generated error as HTML for browsers and as a
// small JSON object for everything else (curl, fetch, API clients).
func (s *Server) sendError(w http.ResponseWriter, r *http.Request, status int, code, message, host string, kind pageKind) {
	w.Header().Set("Vary", "Accept")
	w.Header().Set("Cache-Control", "no-store")
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": code, "host": host, "message": message})
		return
	}
	d := pageData{Tool: brand.Name, Host: host, Message: message}
	switch kind {
	case pageNotFound:
		d.Title, d.Heading = "Not found", "No app at "+host
		d.Hint = "Start one with: " + brand.Name + " " + suggestName(host, s.TLD) + " <command>"
		d.Routes = s.pageRoutes(r)
		if len(d.Routes) > 0 {
			d.Message += " Running apps:"
		}
	case pageStarting:
		d.Title, d.Heading, d.Refresh = "Starting", host+" is starting", true
		d.Hint = "This page reloads automatically once the app is listening."
	case pageRestarting:
		d.Title, d.Heading, d.Refresh = "Unavailable", host+" is not accepting connections", true
		d.Hint = "This page reloads automatically. If it does not recover, check the terminal running " + brand.Name + "."
	case pageBadGateway:
		d.Title, d.Heading = "Bad gateway", host+" is not responding"
		d.Hint = "Check the terminal running " + brand.Name + " for errors."
	case pageLoop:
		d.Title, d.Heading = "Loop detected", "Proxy loop for "+host
		d.Hint = "If a dev server proxies to another app, make it send Host: localhost (for example changeOrigin: true in Vite or webpack proxy settings)."
	case pageMissingHost:
		d.Title, d.Heading = "Bad request", "Missing Host header"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	pageTmpl.Execute(w, d)
}

func suggestName(host, tld string) string {
	if name, ok := strings.CutSuffix(host, "."+tld); ok && name != "" {
		return name
	}
	return "myapp"
}

// pageRoutes lists the routes as links on the scheme and port the request
// came in on, so they work from whichever listener served the page.
func (s *Server) pageRoutes(r *http.Request) []pageRoute {
	scheme, port := requestScheme(r), requestPort(r)
	suffix := ":" + port
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		suffix = ""
	}
	routes := s.Table.Routes()
	sort.Slice(routes, func(i, j int) bool { return routes[i].Hostname < routes[j].Hostname })
	out := make([]pageRoute, 0, len(routes))
	for _, rt := range routes {
		u := scheme + "://" + rt.Hostname + suffix
		detail := "starting"
		if rt.Port > 0 {
			detail = "port " + strconv.Itoa(rt.Port)
		}
		if rt.PID == 0 {
			detail += " · alias"
		} else if !rt.Since.IsZero() {
			detail += " · up " + formatUptime(time.Since(rt.Since))
		}
		out = append(out, pageRoute{Hostname: rt.Hostname, URL: u, Detail: detail})
	}
	return out
}

func formatUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h " + strconv.Itoa(int(d.Minutes())%60) + "m"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}
