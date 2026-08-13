package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"

	contextSubject = "auth.subject"
	contextRole    = "auth.role"
)

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func Issue(secret, subject, role string) (string, error) {
	if !validRole(role) {
		return "", errors.New("invalid role")
	}

	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(8 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(secret))
}

func Middleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": "missing bearer token"},
			)
			return
		}

		rawToken := strings.TrimPrefix(header, "Bearer ")

		claims := &Claims{}

		token, err := jwt.ParseWithClaims(
			rawToken,
			claims,
			func(token *jwt.Token) (any, error) {
				return []byte(secret), nil
			},
			jwt.WithValidMethods([]string{"HS256"}),
		)

		if err != nil || !token.Valid || !validRole(claims.Role) {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": "invalid token"},
			)
			return
		}

		c.Set(contextSubject, claims.Subject)
		c.Set(contextRole, claims.Role)

		c.Next()
	}
}

func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))

	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		role, ok := c.Get(contextRole)
		if !ok {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": "missing authenticated identity"},
			)
			return
		}

		roleString, ok := role.(string)
		if !ok {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": "invalid authenticated identity"},
			)
			return
		}

		if _, ok := allowed[roleString]; !ok {
			c.AbortWithStatusJSON(
				http.StatusForbidden,
				gin.H{"error": "insufficient permissions"},
			)
			return
		}

		c.Next()
	}
}

func Subject(c *gin.Context) string {
	value, _ := c.Get(contextSubject)
	subject, _ := value.(string)
	return subject
}

func Role(c *gin.Context) string {
	value, _ := c.Get(contextRole)
	role, _ := value.(string)
	return role
}

func validRole(role string) bool {
	switch role {
	case RoleViewer, RoleOperator, RoleAdmin:
		return true
	default:
		return false
	}
}
