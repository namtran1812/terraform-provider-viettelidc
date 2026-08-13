package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func testRouter(secret string, requiredRoles ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()

	protected := r.Group(
		"/protected",
		Middleware(secret),
		RequireRole(requiredRoles...),
	)

	protected.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"subject": Subject(c),
			"role":    Role(c),
		})
	})

	return r
}

func TestMissingTokenReturnsUnauthorized(t *testing.T) {
	r := testRouter("secret", RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
}

func TestMalformedTokenReturnsUnauthorized(t *testing.T) {
	r := testRouter("secret", RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-token")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
}

func TestExpiredTokenReturnsUnauthorized(t *testing.T) {
	claims := Claims{
		Role: RoleViewer,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "expired-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}

	token, err := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		claims,
	).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	r := testRouter("secret", RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
}

func TestViewerAllowedOnViewerRoute(t *testing.T) {
	token, err := Issue("secret", "alice", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	r := testRouter("secret", RoleViewer, RoleOperator, RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}

func TestViewerDeniedOperatorRoute(t *testing.T) {
	token, err := Issue("secret", "alice", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	r := testRouter("secret", RoleOperator, RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.Code)
	}
}

func TestOperatorAllowedOperatorRoute(t *testing.T) {
	token, err := Issue("secret", "operator-user", RoleOperator)
	if err != nil {
		t.Fatal(err)
	}

	r := testRouter("secret", RoleOperator, RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}

func TestAdminAllowedEverywhere(t *testing.T) {
	token, err := Issue("secret", "admin-user", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}

	r := testRouter("secret", RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}
