package server

import (
	"context"
	"testing"
)

func TestCRUD(t *testing.T) {
	s := NewMemoryStore()
	v, err := s.Create(context.Background(), Server{ID: "1", Name: "api", Address: "localhost:80"})
	if err != nil || v.ID != "1" {
		t.Fatal(err)
	}
	if _, err = s.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
}
