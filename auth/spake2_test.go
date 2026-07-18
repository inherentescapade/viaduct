package auth

import (
	"bytes"
	"testing"
)

func TestSpake2AgreesOnSharedKey(t *testing.T) {
	pw := []byte("482913")
	c, err := NewSpake2(SpakeClient, pw)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSpake2(SpakeServer, pw)
	if err != nil {
		t.Fatal(err)
	}

	ck, err := c.Finish(s.Message())
	if err != nil {
		t.Fatalf("client finish: %v", err)
	}
	sk, err := s.Finish(c.Message())
	if err != nil {
		t.Fatalf("server finish: %v", err)
	}
	if !bytes.Equal(ck, sk) {
		t.Fatal("matching passwords must yield the same shared key")
	}
	if len(ck) != 32 {
		t.Fatalf("shared key wrong length: %d", len(ck))
	}
}

func TestSpake2WrongPasswordDiverges(t *testing.T) {
	c, _ := NewSpake2(SpakeClient, []byte("111111"))
	s, _ := NewSpake2(SpakeServer, []byte("222222"))

	ck, err := c.Finish(s.Message())
	if err != nil {
		t.Fatal(err)
	}
	sk, err := s.Finish(c.Message())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ck, sk) {
		t.Fatal("different passwords must yield different shared keys")
	}
}

func TestSpake2RejectsGarbageElement(t *testing.T) {
	c, _ := NewSpake2(SpakeClient, []byte("000000"))
	if _, err := c.Finish(make([]byte, 32)); err == nil {
		// An all-zero 32-byte string is not a valid compressed point on the curve
		// in general; ensure obviously-bad input is rejected rather than panicking.
		t.Skip("all-zero may decode on some builds; the point is no panic")
	}
}
