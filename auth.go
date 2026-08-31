package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "vm_manager_session"
const maxLoginFailures = 5

var loginLockDuration = 15 * time.Minute
var loginFailureWindow = 10 * time.Minute

type AuthService struct {
	mu       sync.RWMutex
	cfg      AuthConfig
	attempts map[string]*loginAttempt
}

type loginAttempt struct {
	Failures    int
	FirstFailAt time.Time
	LockedUntil time.Time
}

type sessionPayload struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
	N   string `json:"n"`
}

func NewAuthService(cfg AuthConfig) (*AuthService, error) {
	normalized, err := validateAuthConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &AuthService{cfg: normalized, attempts: map[string]*loginAttempt{}}, nil
}

func (a *AuthService) Enabled() bool {
	if a == nil {
		return false
	}
	cfg := a.Config()
	return cfg.Enabled
}

func (a *AuthService) Config() AuthConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *AuthService) UpdateConfig(cfg AuthConfig) error {
	normalized, err := validateAuthConfig(cfg)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = normalized
	return nil
}

func (a *AuthService) Register(r *gin.Engine) {
	r.GET("/api/auth", a.status)
	r.POST("/api/login", a.login)
	r.POST("/api/logout", a.logout)
}

func (a *AuthService) RegisterPages(r *gin.Engine) {
	r.GET("/", a.indexPage)
	r.GET("/login", a.loginPage)
	r.GET("/public/*filepath", a.publicFile)
}

func (a *AuthService) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			noStore(c)
		}
		if a.Enabled() && isUnsafeMethod(c.Request.Method) && strings.HasPrefix(c.Request.URL.Path, "/api/") {
			if !sameOriginRequest(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid request origin"})
				return
			}
		}
		if !a.Enabled() || !strings.HasPrefix(c.Request.URL.Path, "/api/") || isAuthEndpoint(c.Request.URL.Path) {
			c.Next()
			return
		}
		if _, ok := a.validSession(c); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		c.Next()
	}
}

func isAuthEndpoint(path string) bool {
	return path == "/api/auth" || path == "/api/login" || path == "/api/logout"
}

func isUnsafeMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func sameOriginRequest(c *gin.Context) bool {
	source := c.GetHeader("Origin")
	if source == "" {
		source = c.GetHeader("Referer")
	}
	if source == "" {
		return false
	}

	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" {
		return false
	}
	if !strings.EqualFold(parsed.Host, requestHost(c)) {
		return false
	}

	requestScheme := requestScheme(c)
	if requestScheme == "" {
		return true
	}
	return strings.EqualFold(parsed.Scheme, requestScheme)
}

func requestHost(c *gin.Context) string {
	if host := c.GetHeader("X-Forwarded-Host"); host != "" {
		return strings.TrimSpace(strings.Split(host, ",")[0])
	}
	return c.Request.Host
}

func requestScheme(c *gin.Context) string {
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		return strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

func (a *AuthService) indexPage(c *gin.Context) {
	noStore(c)
	if a.Enabled() {
		if _, ok := a.validSession(c); !ok {
			c.File(publicAssetPath("login.html"))
			return
		}
	}
	c.File(publicAssetPath("index.html"))
}

func (a *AuthService) loginPage(c *gin.Context) {
	noStore(c)
	if !a.Enabled() {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if _, ok := a.validSession(c); ok {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.File(publicAssetPath("login.html"))
}

func (a *AuthService) publicFile(c *gin.Context) {
	noStore(c)
	requested := path.Clean("/" + strings.TrimPrefix(c.Param("filepath"), "/"))
	if requested == "/" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if a.Enabled() {
		if _, ok := a.validSession(c); !ok && !isLoginAsset(requested) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
	}
	c.File(filepath.Join(activePublicDir(), strings.TrimPrefix(requested, "/")))
}

func isLoginAsset(requested string) bool {
	return requested == "/style.css" || requested == "/login.js"
}

func (a *AuthService) status(c *gin.Context) {
	noStore(c)
	user, authenticated := a.validSession(c)
	c.JSON(http.StatusOK, gin.H{
		"enabled":       a.Enabled(),
		"authenticated": !a.Enabled() || authenticated,
		"user":          user,
	})
}

func (a *AuthService) login(c *gin.Context) {
	noStore(c)
	cfg := a.Config()
	if !cfg.Enabled {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	if wait, locked := a.loginLocked(c); locked {
		c.Header("Retry-After", fmt.Sprintf("%.0f", wait.Seconds()))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts, please try again later"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login request"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Username), []byte(cfg.Username)) != 1 ||
		bcrypt.CompareHashAndPassword([]byte(cfg.PasswordHash), []byte(req.Password)) != nil {
		a.recordLoginFailure(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	a.clearLoginFailures(c)
	if err := a.setSessionCookie(c, req.Username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (a *AuthService) setSessionCookie(c *gin.Context, username string) error {
	cfg := a.Config()
	token, err := a.newSessionToken(username)
	if err != nil {
		return err
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   cfg.SessionHours * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cfg.CookieSecure,
	})
	return nil
}

func (a *AuthService) logout(c *gin.Context) {
	noStore(c)
	cfg := a.Config()
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cfg.CookieSecure,
	})
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (a *AuthService) newSessionToken(username string) (string, error) {
	cfg := a.Config()
	nonce := make([]byte, 18)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := sessionPayload{
		Sub: username,
		Exp: time.Now().Add(time.Duration(cfg.SessionHours) * time.Hour).Unix(),
		N:   base64.RawURLEncoding.EncodeToString(nonce),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return encoded + "." + signValue(encoded, cfg.SessionSecret), nil
}

func (a *AuthService) validSession(c *gin.Context) (string, bool) {
	cfg := a.Config()
	if !cfg.Enabled {
		return "", true
	}
	cookie, err := c.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	encoded, sig, ok := strings.Cut(cookie, ".")
	if !ok || !hmac.Equal([]byte(signValue(encoded, cfg.SessionSecret)), []byte(sig)) {
		return "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	var payload sessionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", false
	}
	if payload.Sub != cfg.Username || time.Now().Unix() > payload.Exp {
		return "", false
	}
	return payload.Sub, true
}

func signValue(value, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *AuthService) loginAttemptKey(c *gin.Context) string {
	return c.ClientIP()
}

func (a *AuthService) loginLocked(c *gin.Context) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt, ok := a.attempts[a.loginAttemptKey(c)]
	if !ok || attempt.LockedUntil.IsZero() {
		return 0, false
	}
	now := time.Now()
	if now.After(attempt.LockedUntil) {
		delete(a.attempts, a.loginAttemptKey(c))
		return 0, false
	}
	return time.Until(attempt.LockedUntil), true
}

func (a *AuthService) recordLoginFailure(c *gin.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := a.loginAttemptKey(c)
	now := time.Now()
	attempt, ok := a.attempts[key]
	if !ok || now.Sub(attempt.FirstFailAt) > loginFailureWindow {
		attempt = &loginAttempt{FirstFailAt: now}
		a.attempts[key] = attempt
	}
	attempt.Failures++
	if attempt.Failures >= maxLoginFailures {
		attempt.LockedUntil = now.Add(loginLockDuration)
	}
}

func (a *AuthService) clearLoginFailures(c *gin.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.attempts, a.loginAttemptKey(c))
}

func validateAuthConfig(cfg AuthConfig) (AuthConfig, error) {
	if !cfg.Enabled {
		return cfg, nil
	}
	if cfg.Username == "" {
		return cfg, fmt.Errorf("auth username is required")
	}
	if cfg.PasswordHash == "" {
		return cfg, fmt.Errorf("auth password_hash is required")
	}
	if cfg.SessionSecret == "" || len(cfg.SessionSecret) < 32 {
		return cfg, fmt.Errorf("auth session_secret must be at least 32 characters")
	}
	if cfg.SessionHours <= 0 {
		cfg.SessionHours = 12
	}
	return cfg, nil
}
