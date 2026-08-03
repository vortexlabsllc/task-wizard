package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"taskwiz.app/core/config"
	"taskwiz.app/core/internal/models"
	sRepo "taskwiz.app/core/internal/repos/session"
	uRepo "taskwiz.app/core/internal/repos/user"
	"taskwiz.app/core/internal/services/logging"
	"taskwiz.app/core/internal/telemetry"
	authUtils "taskwiz.app/core/internal/utils/auth"
)

const SessionCookieName = "tw_session"
const entraLoginURL = "https://login.microsoftonline.com/"

// clockSkewLeeway tolerates small clock differences between the identity
// provider and this server when validating time-based token claims.
const clockSkewLeeway = 2 * time.Minute

type AuthMiddleware struct {
	enabled     bool
	keySet      oidc.KeySet
	issuer      string
	audience    string
	clientID    string
	multiTenant bool
	userRepo    uRepo.IUserRepo
	sessionRepo sRepo.ISessionRepo
}

type accessTokenClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	ExpiresAt int64  `json:"exp"`
	NotBefore int64  `json:"nbf"`
	TenantID  string `json:"tid"`
	ObjectID  string `json:"oid"`
}

func validateTemporalClaims(claims accessTokenClaims, now time.Time, leeway time.Duration) error {
	if now.Add(-leeway).Unix() > claims.ExpiresAt {
		return fmt.Errorf("token has expired")
	}

	if claims.NotBefore != 0 && now.Add(leeway).Unix() < claims.NotBefore {
		return fmt.Errorf("token is not yet valid")
	}

	return nil
}

func NewAuthMiddleware(cfg *config.Config, userRepo uRepo.IUserRepo, sessionRepo sRepo.ISessionRepo) (*AuthMiddleware, error) {
	m := &AuthMiddleware{
		enabled:     cfg.Entra.Enabled,
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
	}

	if !cfg.Entra.Enabled {
		if cfg.Server.HostName != "" && !cfg.Server.AllowInsecureNoAuth {
			return nil, fmt.Errorf("authentication is disabled (entra.enabled=false) while server.host_name is set (%q); refusing to start in a production-like configuration. Set server.allow_insecure_no_auth=true (env TW_ALLOW_INSECURE_NO_AUTH=true) to explicitly opt in to the insecure dev bypass", cfg.Server.HostName)
		}

		if cfg.Server.AllowInsecureNoAuth {
			logging.DefaultLogger().Error("SECURITY WARNING: authentication is disabled and the insecure no-auth bypass is explicitly enabled (server.allow_insecure_no_auth=true). Every request resolves to a single shared dev user. Never use this in production.")
		}

		return m, nil
	}

	issuer := strings.TrimSpace(cfg.Entra.Issuer)
	if issuer == "" {
		issuer = entraLoginURL + cfg.Entra.AuthorityTenantID() + "/v2.0"
	}
	m.issuer = issuer
	m.audience = cfg.Entra.Audience
	m.clientID = cfg.Entra.ClientID
	m.multiTenant = strings.TrimSpace(cfg.Entra.TenantID) == "" && strings.TrimSpace(cfg.Entra.Issuer) == ""

	provider, err := oidc.NewProvider(context.Background(), issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %s", err.Error())
	}

	var providerClaims struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&providerClaims); err != nil {
		return nil, fmt.Errorf("failed to extract JWKS URI: %s", err.Error())
	}

	m.keySet = oidc.NewRemoteKeySet(context.Background(), providerClaims.JWKSURL)

	return m, nil
}

func (m *AuthMiddleware) MiddlewareFunc() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, err := m.authenticate(c)
		if err != nil {
			telemetry.TrackWarning(c, "auth_unauthorized", "auth-middleware", err.Error(), nil)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.Set(authUtils.IdentityKey, identity)
		c.Next()
	}
}

func (m *AuthMiddleware) authenticate(c *gin.Context) (*models.SignedInIdentity, error) {
	if !m.enabled {
		return m.bypassAuth(c.Request.Context())
	}

	// Prefer bearer token when present (explicit auth takes precedence)
	token := extractBearerToken(c)
	if token != "" {
		return m.verifyAccessToken(c.Request.Context(), token)
	}

	// Fall back to session cookie
	if sessionToken, err := c.Cookie(SessionCookieName); err == nil && sessionToken != "" {
		identity, err := m.authenticateSession(c.Request.Context(), sessionToken)
		if err == nil {
			return identity, nil
		}
	}

	return nil, fmt.Errorf("missing authorization token")
}

func extractBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(authHeader, "Bearer ")
}

func (m *AuthMiddleware) verifyAccessToken(ctx context.Context, rawToken string) (*models.SignedInIdentity, error) {
	payload, err := m.keySet.VerifySignature(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("token signature verification failed: %s", err.Error())
	}

	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse token claims: %s", err.Error())
	}

	if !m.validIssuer(claims) {
		return nil, fmt.Errorf("invalid token issuer")
	}

	if claims.Audience != m.audience && claims.Audience != m.clientID {
		return nil, fmt.Errorf("invalid token audience")
	}

	if err := validateTemporalClaims(claims, time.Now(), clockSkewLeeway); err != nil {
		return nil, err
	}

	if claims.TenantID == "" || claims.ObjectID == "" {
		return nil, fmt.Errorf("missing tid or oid in token claims")
	}

	user, err := m.userRepo.EnsureUser(ctx, claims.TenantID, claims.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user identity: %s", err.Error())
	}

	return &models.SignedInIdentity{
		UserID:          user.ID,
		Type:            models.IdentityTypeUser,
		Scopes:          models.AllUserScopes(),
		PendingDeletion: user.DeletionRequestedAt != nil,
	}, nil
}

func (m *AuthMiddleware) validIssuer(claims accessTokenClaims) bool {
	if !m.multiTenant {
		return claims.Issuer == m.issuer
	}

	return claims.Issuer == entraLoginURL+claims.TenantID+"/v2.0"
}

func (m *AuthMiddleware) authenticateSession(ctx context.Context, rawToken string) (*models.SignedInIdentity, error) {
	session, err := m.sessionRepo.ValidateSession(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("invalid session: %s", err.Error())
	}

	user, err := m.userRepo.GetUser(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve session user: %s", err.Error())
	}

	if user.Disabled {
		return nil, fmt.Errorf("account is disabled")
	}

	return &models.SignedInIdentity{
		UserID:          user.ID,
		Type:            models.IdentityTypeUser,
		Scopes:          models.AllUserScopes(),
		PendingDeletion: user.DeletionRequestedAt != nil,
	}, nil
}

func (m *AuthMiddleware) bypassAuth(ctx context.Context) (*models.SignedInIdentity, error) {
	user, err := m.userRepo.EnsureUser(ctx, "dev-directory", "dev-object")
	if err != nil {
		return nil, fmt.Errorf("failed to ensure dev user: %s", err.Error())
	}

	return &models.SignedInIdentity{
		UserID:          user.ID,
		Type:            models.IdentityTypeUser,
		Scopes:          models.AllUserScopes(),
		PendingDeletion: user.DeletionRequestedAt != nil,
	}, nil
}

// VerifyWSToken validates a token for WebSocket connections.
func (m *AuthMiddleware) VerifyWSToken(ctx context.Context, rawToken string) (*models.SignedInIdentity, error) {
	if !m.enabled {
		return m.bypassAuth(ctx)
	}

	return m.verifyAccessToken(ctx, rawToken)
}

func ScopeMiddleware(requiredScope models.ApiTokenScope) gin.HandlerFunc {
	return func(c *gin.Context) {
		currentIdentity := authUtils.CurrentIdentity(c)

		if slices.Contains(currentIdentity.Scopes, requiredScope) {
			c.Next()
			return
		}

		telemetry.TrackWarning(c, "auth_forbidden_scope", "auth-middleware", "Missing required scope: "+string(requiredScope), nil)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "Missing required scope: " + string(requiredScope),
		})
	}
}
