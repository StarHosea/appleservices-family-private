package dashboard

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net/http"
	"strings"
	"time"
)

const webSessionLifetime = 12 * time.Hour

var loginTemplate = template.Must(template.ParseFS(assets, "web/admin-login.html"))

func (s *Server) sessionCookieName() string {
	if strings.HasPrefix(s.URL(), "https://") {
		return "__Host-apple-services-admin"
	}
	return "apple-services-admin"
}

func sessionHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Server) webAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(s.sessionCookieName())
	if err != nil || len(cookie.Value) != 64 {
		return false
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	key := sessionHash(cookie.Value)
	expires, ok := s.webSessions[key]
	if !ok || !time.Now().Before(expires) {
		delete(s.webSessions, key)
		return false
	}
	return true
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, username, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	loginTemplate.Execute(w, struct{ Username, Error string }{username, message})
}

// serveWebAuth handles only administrator web login. The account-login API
// and dedicated session-export credential remain separate.
func (s *Server) serveWebAuth(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/login.css":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		data, _ := assets.ReadFile("web/admin-login.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(data)
		return true
	case "/login":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if s.config.AdminToken == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return true
		}
		if r.Method == http.MethodGet {
			if s.webAuthorized(r) {
				http.Redirect(w, r, "/", http.StatusSeeOther)
			} else {
				s.renderLogin(w, http.StatusOK, "", "")
			}
			return true
		}
		// Native form submissions send Origin. Reject absent/foreign origins
		// rather than allowing a third-party page to log a browser in.
		if r.Header.Get("Origin") != s.URL() {
			s.renderLogin(w, http.StatusForbidden, "", "请求来源无效，请重新打开登录页面。")
			return true
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") || r.ParseForm() != nil {
			s.renderLogin(w, http.StatusBadRequest, "", "登录信息无效，请重新输入。")
			return true
		}
		username, password := r.PostForm.Get("username"), r.PostForm.Get("password")
		s.authMu.Lock()
		now := time.Now()
		if now.Sub(s.loginWindow) >= time.Minute {
			s.loginWindow, s.loginFailures = now, 0
		}
		if s.loginFailures >= 10 {
			s.authMu.Unlock()
			w.Header().Set("Retry-After", "60")
			s.renderLogin(w, http.StatusTooManyRequests, username, "尝试次数过多，请一分钟后重试。")
			return true
		}
		adminPassword := s.config.AdminPassword
		if adminPassword == "" {
			adminPassword = s.config.AdminToken
		}
		validUser, validPassword := equalSecret(username, s.config.AdminUsername), equalSecret(password, adminPassword)
		if !validUser || !validPassword {
			s.loginFailures++
			s.authMu.Unlock()
			s.renderLogin(w, http.StatusUnauthorized, username, "用户名或密码不正确。")
			return true
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			s.authMu.Unlock()
			s.renderLogin(w, http.StatusServiceUnavailable, username, "暂时无法登录，请稍后重试。")
			return true
		}
		if s.webSessions == nil {
			s.webSessions = make(map[string]time.Time)
		}
		for hash, expires := range s.webSessions {
			if !now.Before(expires) {
				delete(s.webSessions, hash)
			}
		}
		// Rotate the current browser's session on every successful login.
		if previous, err := r.Cookie(s.sessionCookieName()); err == nil {
			delete(s.webSessions, sessionHash(previous.Value))
		}
		if len(s.webSessions) >= 128 {
			s.authMu.Unlock()
			s.renderLogin(w, http.StatusServiceUnavailable, username, "登录会话已满，请稍后重试。")
			return true
		}
		value, expires := hex.EncodeToString(key), now.Add(webSessionLifetime)
		s.webSessions[sessionHash(value)] = expires
		s.loginFailures = 0
		s.authMu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Value: value, Path: "/", HttpOnly: true,
			Secure: strings.HasPrefix(s.URL(), "https://"), SameSite: http.SameSiteStrictMode, MaxAge: int(webSessionLifetime.Seconds()), Expires: expires})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return true
	case "/logout":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if r.Header.Get("Origin") != s.URL() {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return true
		}
		if cookie, err := r.Cookie(s.sessionCookieName()); err == nil {
			s.authMu.Lock()
			delete(s.webSessions, sessionHash(cookie.Value))
			s.authMu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Path: "/", MaxAge: -1, HttpOnly: true,
			Secure: strings.HasPrefix(s.URL(), "https://"), SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return true
	}
	return false
}
