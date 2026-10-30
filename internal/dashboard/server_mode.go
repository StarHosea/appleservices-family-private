package dashboard

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func listenAddress(config Config) (string, error) {
	address := config.Listen
	if address == "" {
		address = net.JoinHostPort("127.0.0.1", intString(config.Port))
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return "", errors.New("invalid listen address; use host:port")
	}
	ip := net.ParseIP(host)
	remote := ip == nil || !ip.IsLoopback()
	if config.PublicURL != "" || remote || config.AdminToken != "" {
		origin, err := url.Parse(config.PublicURL)
		if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") ||
			origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			return "", errors.New("server mode requires a public origin without a path, credentials or query")
		}
		if len(config.AdminToken) < 32 || strings.ContainsAny(config.AdminToken, "\r\n") {
			return "", errors.New("server mode requires an admin token of at least 32 characters")
		}
	}
	return address, nil
}

func equalSecret(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

func (s *Server) bearerAuthorized(r *http.Request) bool {
	value, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return found && s.config.AdminToken != "" && equalSecret(value, s.config.AdminToken)
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.config.AdminToken == "" || s.bearerAuthorized(r) {
		return true
	}
	user, password, ok := r.BasicAuth()
	if ok && user == "admin" && equalSecret(password, s.config.AdminToken) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="Apple Services", charset="UTF-8"`)
	http.Error(w, "authentication required", http.StatusUnauthorized)
	return false
}
