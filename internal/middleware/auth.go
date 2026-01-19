package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	// ContextKeyUserID is the key for user ID in gin context
	ContextKeyUserID = "user_id"
	// ContextKeyUserClaims is the key for JWT claims in gin context
	ContextKeyUserClaims = "user_claims"
)

// JWTClaims represents the claims in a JWT token.
type JWTClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// AuthConfig holds configuration for JWT authentication.
type AuthConfig struct {
	SecretKey      string        // JWT secret key
	ExpiryDuration time.Duration // Token expiry duration
	Issuer         string        // Token issuer
	Audience       string        // Token audience
	TokenHeader    string        // Header name for token
	TokenPrefix    string        // Prefix before token (e.g., "Bearer ")
	SkipPaths      []string      // Paths that don't require authentication
}

// DefaultAuthConfig returns default authentication configuration.
func DefaultAuthConfig() *AuthConfig {
	return &AuthConfig{
		SecretKey:      "your-secret-key-change-in-production",
		ExpiryDuration: 24 * time.Hour,
		Issuer:         "limit-order-book",
		Audience:       "limit-order-book-api",
		TokenHeader:    "Authorization",
		TokenPrefix:    "Bearer ",
		SkipPaths: []string{
			"/health",
			"/ready",
			"/api/currencies",
			"/api/pairs",
			"/api/pairs/:pair/book",
			"/api/pairs/:pair/ticker",
		},
	}
}

// AuthMiddleware provides JWT authentication for Gin.
// USAGE:
//   - Validates JWT tokens from Authorization header
//   - Extracts user information and adds to context
//   - Skips authentication for configured paths
type AuthMiddleware struct {
	config *AuthConfig
}

// NewAuthMiddleware creates a new authentication middleware.
func NewAuthMiddleware(config *AuthConfig) *AuthMiddleware {
	if config == nil {
		config = DefaultAuthConfig()
	}
	return &AuthMiddleware{config: config}
}

// GinMiddleware returns the Gin middleware handler function.
func (a *AuthMiddleware) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip authentication for certain paths
		if a.shouldSkip(c.Request.URL.Path) {
			c.Next()
			return
		}

		// Extract token from header
		authHeader := c.GetHeader(a.config.TokenHeader)
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "missing authorization header",
				"code":    "AUTH_MISSING_HEADER",
			})
			return
		}

		// Check for Bearer prefix
		if !strings.HasPrefix(authHeader, a.config.TokenPrefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "invalid authorization header format",
				"code":    "AUTH_INVALID_FORMAT",
			})
			return
		}

		// Extract token
		tokenString := strings.TrimPrefix(authHeader, a.config.TokenPrefix)

		// Parse and validate token
		claims, err := a.validateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
				"code":    "AUTH_INVALID_TOKEN",
			})
			return
		}

		// Add claims to context
		c.Set(ContextKeyUserID, claims.UserID)
		c.Set(ContextKeyUserClaims, claims)

		c.Next()
	}
}

// shouldSkip checks if the path should skip authentication.
func (a *AuthMiddleware) shouldSkip(path string) bool {
	for _, skipPath := range a.config.SkipPaths {
		if path == skipPath {
			return true
		}
		// Check for path with parameters
		if strings.HasPrefix(path, skipPath[:len(skipPath)-1]) && skipPath[len(skipPath)-1] == ':' {
			return true
		}
	}
	return false
}

// validateToken parses and validates a JWT token.
func (a *AuthMiddleware) validateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return []byte(a.config.SecretKey), nil
	})

	if err != nil {
		return nil, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	// Validate issuer and audience if configured
	if a.config.Issuer != "" {
		issuer, err := claims.GetIssuer()
		if err != nil || issuer != a.config.Issuer {
			return nil, errors.New("invalid token issuer")
		}
	}

	if a.config.Audience != "" {
		audience, err := claims.GetAudience()
		if err != nil || !containsAudience(audience, a.config.Audience) {
			return nil, errors.New("invalid token audience")
		}
	}

	return claims, nil
}

// containsAudience checks if the audience slice contains the expected audience.
func containsAudience(audiences []string, expected string) bool {
	for _, aud := range audiences {
		if aud == expected {
			return true
		}
	}
	return false
}

// GenerateToken generates a new JWT token for a user.
func (a *AuthMiddleware) GenerateToken(userID int64, username, role string) (string, error) {
	now := time.Now()
	claims := &JWTClaims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(a.config.ExpiryDuration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    a.config.Issuer,
			Audience:  jwt.ClaimStrings{a.config.Audience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.SecretKey))
}

// GetUserID extracts the user ID from gin context.
func GetUserID(c *gin.Context) (int64, bool) {
	userID, exists := c.Get(ContextKeyUserID)
	if !exists {
		return 0, false
	}
	id, ok := userID.(int64)
	return id, ok
}

// GetUserClaims extracts the JWT claims from gin context.
func GetUserClaims(c *gin.Context) (*JWTClaims, bool) {
	claims, exists := c.Get(ContextKeyUserClaims)
	if !exists {
		return nil, false
	}
	jwtClaims, ok := claims.(*JWTClaims)
	return jwtClaims, ok
}

// APIKeyAuth provides API key-based authentication.
// Useful for service-to-service authentication.
type APIKeyAuth struct {
	validKeys map[string]*APIKeyInfo
}

// APIKeyInfo holds information about an API key.
type APIKeyInfo struct {
	KeyID      string
	Secret     string
	UserID     int64
	Role       string
	Scopes     []string
	Expiration time.Time
}

// NewAPIKeyAuth creates a new API key authentication handler.
func NewAPIKeyAuth() *APIKeyAuth {
	return &APIKeyAuth{
		validKeys: make(map[string]*APIKeyInfo),
	}
}

// AddKey adds a new valid API key.
func (a *APIKeyAuth) AddKey(info *APIKeyInfo) {
	a.validKeys[info.KeyID] = info
}

// RemoveKey removes an API key.
func (a *APIKeyAuth) RemoveKey(keyID string) {
	delete(a.validKeys, keyID)
}

// GinMiddleware returns the Gin middleware for API key authentication.
func (a *APIKeyAuth) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "missing API key",
				"code":    "API_KEY_MISSING",
			})
			return
		}

		info, valid := a.validKeys[apiKey]
		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "invalid API key",
				"code":    "API_KEY_INVALID",
			})
			return
		}

		// Check expiration
		if !info.Expiration.IsZero() && time.Now().After(info.Expiration) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "API key expired",
				"code":    "API_KEY_EXPIRED",
			})
			return
		}

		// Add API key info to context
		c.Set("api_key_info", info)
		c.Set(ContextKeyUserID, info.UserID)
		c.Set("api_key_scopes", info.Scopes)

		c.Next()
	}
}

// HasScope checks if the request has a specific scope.
func HasScope(c *gin.Context, scope string) bool {
	scopes, exists := c.Get("api_key_scopes")
	if !exists {
		return false
	}
	scopesList, ok := scopes.([]string)
	if !ok {
		return false
	}
	for _, s := range scopesList {
		if s == scope {
			return true
		}
	}
	return false
}

// CombinedAuth combines JWT and API key authentication.
// Tries API key first, then JWT.
type CombinedAuth struct {
	jwtAuth *AuthMiddleware
	apiKey  *APIKeyAuth
}

// NewCombinedAuth creates a new combined authentication handler.
func NewCombinedAuth(jwtConfig, apiKeyConfig *AuthConfig) *CombinedAuth {
	return &CombinedAuth{
		jwtAuth: NewAuthMiddleware(jwtConfig),
		apiKey:  NewAPIKeyAuth(),
	}
}

// GinMiddleware returns the combined authentication middleware.
func (c *CombinedAuth) GinMiddleware() gin.HandlerFunc {
	return func(cxt *gin.Context) {
		// Try API key first
		apiKey := cxt.GetHeader("X-API-Key")
		if apiKey != "" {
			cxt.Set("auth_method", "api_key")
			c.apiKey.GinMiddleware()(cxt)
			return
		}

		// Fall back to JWT
		cxt.Set("auth_method", "jwt")
		c.jwtAuth.GinMiddleware()(cxt)
	}
}
