package config

import "testing"

func TestJWKSDerivedFromIssuer(t *testing.T) {
	t.Setenv("DREAMSYNC_IAM_ISSUER", "https://iam.example.com/")
	t.Setenv("DREAMSYNC_JWKS_URI", "")
	cfg := Load()
	if got, want := cfg.JWKSURI, "https://iam.example.com/realms/user/jwks"; got != want {
		t.Fatalf("JWKSURI = %q, want %q", got, want)
	}
}

func TestJWKSExplicitWins(t *testing.T) {
	t.Setenv("DREAMSYNC_IAM_ISSUER", "https://iam.example.com")
	t.Setenv("DREAMSYNC_JWKS_URI", "https://keys.example.com/jwks")
	if got := Load().JWKSURI; got != "https://keys.example.com/jwks" {
		t.Fatalf("JWKSURI = %q, want explicit value", got)
	}
}

func TestMaxDocBytesClampsNonPositive(t *testing.T) {
	// A non-positive cap would make http.MaxBytesReader reject every upload;
	// Load must clamp it back to the default.
	for _, v := range []string{"-1", "0", "notanumber"} {
		t.Setenv("DREAMSYNC_MAX_DOC_BYTES", v)
		if got := Load().MaxDocBytes; got != defaultMaxDocBytes {
			t.Fatalf("MaxDocBytes for %q = %d, want default %d", v, got, defaultMaxDocBytes)
		}
	}
}

func TestMaxDocBytesHonoursValid(t *testing.T) {
	t.Setenv("DREAMSYNC_MAX_DOC_BYTES", "1048576")
	if got := Load().MaxDocBytes; got != 1048576 {
		t.Fatalf("MaxDocBytes = %d, want 1048576", got)
	}
}
