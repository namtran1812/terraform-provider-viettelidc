package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimitAllowsBurst(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := New(1, 2, time.Minute)

	r := gin.New()
	r.Use(manager.Middleware())

	r.GET("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"

		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}
	}
}

func TestRateLimitRejectsExcessRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := New(1, 1, time.Minute)

	r := gin.New()
	r.Use(manager.Middleware())

	r.GET("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	first := httptest.NewRequest(http.MethodGet, "/", nil)
	first.RemoteAddr = "127.0.0.1:1234"

	firstResp := httptest.NewRecorder()
	r.ServeHTTP(firstResp, first)

	second := httptest.NewRequest(http.MethodGet, "/", nil)
	second.RemoteAddr = "127.0.0.1:1234"

	secondResp := httptest.NewRecorder()
	r.ServeHTTP(secondResp, second)

	if secondResp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", secondResp.Code)
	}
}
