package postgres

import (
	"bytes"
	"testing"
)

func TestSessionCodecEncryptsAndAuthenticatesState(t *testing.T) {
	codec, err := newSessionCodec(bytes.Repeat([]byte{0x2a}, 32))
	if err != nil {
		t.Fatalf("create session codec: %v", err)
	}
	value := struct {
		AccessToken string `json:"access_token"`
	}{AccessToken: "sentinel-access-token"}
	aad := appendAAD("session", []byte("session-hash"), []byte("principal-id"))
	envelope, err := codec.encryptJSON(value, aad)
	if err != nil {
		t.Fatalf("encrypt state: %v", err)
	}
	if bytes.Contains(envelope, []byte(value.AccessToken)) {
		t.Fatal("encrypted state contains plaintext token")
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
	}
	if err := codec.decryptJSON(envelope, aad, &decoded); err != nil || decoded.AccessToken != value.AccessToken {
		t.Fatalf("decrypt state: decoded=%+v err=%v", decoded, err)
	}
	if err := codec.decryptJSON(envelope, appendAAD("session", []byte("other")), &decoded); err == nil {
		t.Fatal("expected additional-data mismatch rejection")
	}
	tampered := append([]byte(nil), envelope...)
	tampered[len(tampered)-1] ^= 1
	if err := codec.decryptJSON(tampered, aad, &decoded); err == nil {
		t.Fatal("expected tampered envelope rejection")
	}
}

func TestSessionCodecSeparatesHashesAndCSRFProofs(t *testing.T) {
	codec, err := newSessionCodec(bytes.Repeat([]byte{0x19}, 32))
	if err != nil {
		t.Fatalf("create session codec: %v", err)
	}
	session := bytes.Repeat([]byte{0x03}, randomCredentialBytes)
	if bytes.Equal(keyedHash(codec.sessionHashKey, session), keyedHash(codec.browserHashKey, session)) {
		t.Fatal("session and browser hashes must be domain separated")
	}
	if codec.csrfToken(session) == codec.csrfToken(bytes.Repeat([]byte{0x04}, randomCredentialBytes)) {
		t.Fatal("CSRF proof must be bound to one session")
	}
}

func TestSessionCodecRejectsShortMasterKey(t *testing.T) {
	if _, err := newSessionCodec([]byte("short")); err == nil {
		t.Fatal("expected short master key rejection")
	}
}
