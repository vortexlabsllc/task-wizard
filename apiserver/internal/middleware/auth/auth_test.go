package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"taskwiz.app/core/config"
)

func newDisabledAuthConfig(hostName string, allowInsecure bool) *config.Config {
	return &config.Config{
		Entra: config.EntraConfig{Enabled: false},
		Server: config.ServerConfig{
			HostName:            hostName,
			AllowInsecureNoAuth: allowInsecure,
		},
	}
}

func TestNewAuthMiddleware_DisabledWithHostNameAndNoOptIn_Fails(t *testing.T) {
	cfg := newDisabledAuthConfig("example.com", false)

	m, err := NewAuthMiddleware(cfg, nil, nil)

	assert.Error(t, err)
	assert.Nil(t, m)
}

func TestNewAuthMiddleware_DisabledWithOptIn_Allowed(t *testing.T) {
	cfg := newDisabledAuthConfig("example.com", true)

	m, err := NewAuthMiddleware(cfg, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, m)
	assert.False(t, m.enabled)
}

func TestNewAuthMiddleware_DisabledWithoutHostName_Allowed(t *testing.T) {
	cfg := newDisabledAuthConfig("", false)

	m, err := NewAuthMiddleware(cfg, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, m)
	assert.False(t, m.enabled)
}

func TestValidateTemporalClaims_WithinWindow_Valid(t *testing.T) {
	now := time.Now()
	claims := accessTokenClaims{
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
		NotBefore: now.Add(-10 * time.Minute).Unix(),
	}

	assert.NoError(t, validateTemporalClaims(claims, now, clockSkewLeeway))
}

func TestValidateTemporalClaims_ExpiredWithinLeeway_Valid(t *testing.T) {
	now := time.Now()
	claims := accessTokenClaims{
		ExpiresAt: now.Add(-1 * time.Minute).Unix(),
	}

	assert.NoError(t, validateTemporalClaims(claims, now, clockSkewLeeway))
}

func TestValidateTemporalClaims_ExpiredBeyondLeeway_Invalid(t *testing.T) {
	now := time.Now()
	claims := accessTokenClaims{
		ExpiresAt: now.Add(-5 * time.Minute).Unix(),
	}

	err := validateTemporalClaims(claims, now, clockSkewLeeway)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestValidateTemporalClaims_NotBeforeWithinLeeway_Valid(t *testing.T) {
	now := time.Now()
	claims := accessTokenClaims{
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
		NotBefore: now.Add(1 * time.Minute).Unix(),
	}

	assert.NoError(t, validateTemporalClaims(claims, now, clockSkewLeeway))
}

func TestValidateTemporalClaims_NotBeforeBeyondLeeway_Invalid(t *testing.T) {
	now := time.Now()
	claims := accessTokenClaims{
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
		NotBefore: now.Add(5 * time.Minute).Unix(),
	}

	err := validateTemporalClaims(claims, now, clockSkewLeeway)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet valid")
}

func TestValidateTemporalClaims_NotBeforeAbsent_Valid(t *testing.T) {
	now := time.Now()
	claims := accessTokenClaims{
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
	}

	assert.NoError(t, validateTemporalClaims(claims, now, clockSkewLeeway))
}

func TestAuthMiddlewareValidIssuer(t *testing.T) {
	const tenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"

	t.Run("accepts a tenant-specific issuer in multi-tenant mode", func(t *testing.T) {
		middleware := &AuthMiddleware{multiTenant: true}
		claims := accessTokenClaims{
			Issuer:   entraLoginURL + tenantID + "/v2.0",
			TenantID: tenantID,
		}

		assert.True(t, middleware.validIssuer(claims))
	})

	t.Run("rejects an issuer that does not match the token tenant", func(t *testing.T) {
		middleware := &AuthMiddleware{multiTenant: true}
		claims := accessTokenClaims{
			Issuer:   entraLoginURL + "other-tenant/v2.0",
			TenantID: tenantID,
		}

		assert.False(t, middleware.validIssuer(claims))
	})

	t.Run("requires the configured issuer in single-tenant mode", func(t *testing.T) {
		middleware := &AuthMiddleware{
			issuer: entraLoginURL + "configured-tenant/v2.0",
		}
		claims := accessTokenClaims{
			Issuer:   entraLoginURL + tenantID + "/v2.0",
			TenantID: tenantID,
		}

		assert.False(t, middleware.validIssuer(claims))
	})
}
