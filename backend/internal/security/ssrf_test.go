package security

import (
	"testing"
)

func TestValidateWebFetchURL_BlocksPrivate(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/",
		"https://192.168.1.1/foo",
		"http://10.0.0.1/",
		"http://[::1]/",
		"http://[fe80::1]/",
		"http://169.254.169.254/latest/meta-data/",
		"http://localhost/foo",
		"https://example.com:8080/", // non-80/443
	}
	for _, u := range cases {
		if err := ValidateWebFetchURL(u); err == nil {
			t.Fatalf("expected error for %q", u)
		}
	}
}

func TestValidateWebFetchURL_AllowsPublicIP(t *testing.T) {
	// Literal public IP: no DNS lookup required (stable in CI/offline).
	if err := ValidateWebFetchURL("https://1.1.1.1/"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWebFetchURL("http://1.1.1.1:80/"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWebFetchURL_RejectsUserinfo(t *testing.T) {
	if err := ValidateWebFetchURL("https://user:pass@example.com/"); err == nil {
		t.Fatal("expected error")
	}
}
