package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

// A rejection is a framework turning a request away because of the Host
// or Origin aka sends. Each has a recognisable error body and a known fix.
type rejection struct {
	status []int
	marks  []string // any one, case-insensitive
	hint   string
}

var rejections = []rejection{
	{[]int{400}, []string{"DisallowedHost", "Invalid HTTP_HOST header"},
		`Django rejected the host. Add ".localhost" to ALLOWED_HOSTS in your settings (a leading dot allows every *.localhost name).`},
	{[]int{403}, []string{"Origin checking failed"},
		`Django's CSRF check failed because it sees http behind the HTTPS proxy. Add SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https") to your development settings.`},
	{[]int{403}, []string{"Blocked request. This host"},
		`Vite rejected the host. Add it to server.allowedHosts in vite.config (for example allowedHosts: [".localhost"]).`},
	{[]int{400, 403}, []string{"Invalid Host header"},
		`The dev server (webpack-dev-server) rejected the host. Set devServer.allowedHosts to [".localhost"] (Angular: ng serve --allowed-hosts .localhost).`},
	{[]int{403}, []string{"Blocked hosts:", "Blocked host:"},
		`Rails host authorization rejected the host. Add config.hosts << ".localhost" to config/environments/development.rb.`},
	{[]int{400}, []string{"Invalid Hostname"},
		`ASP.NET host filtering rejected the host. Set "AllowedHosts": "*" (or include the .localhost name) in appsettings.Development.json.`},
}

// maxSniff bounds how much of an error body is inspected.
const maxSniff = 64 << 10

// Diagnose returns a fix for a response that looks like a framework
// rejecting the request's host or origin, or "".
func Diagnose(status int, body []byte) string {
	lower := bytes.ToLower(body)
	for _, r := range rejections {
		if !containsInt(r.status, status) {
			continue
		}
		for _, m := range r.marks {
			if bytes.Contains(lower, []byte(strings.ToLower(m))) {
				return r.hint
			}
		}
	}
	return ""
}

// sniffRejection inspects a 400/403/421 response without consuming it: the
// body prefix that was read is put back in front of the rest.
func sniffRejection(resp *http.Response) string {
	switch resp.StatusCode {
	case 400, 403, 421:
	default:
		return ""
	}
	prefix, err := io.ReadAll(io.LimitReader(resp.Body, maxSniff))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(prefix), resp.Body), resp.Body}
	if err != nil {
		return ""
	}
	return Diagnose(resp.StatusCode, prefix)
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
