package urlguard

import (
	"context"
	"net"
	"testing"
)

func TestValidateRejectsUnsafeURLs(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"gopher://example.com/",
		"ftp://example.com/file.pdf",
		"http://localhost:9000/api/health",
		"http://127.0.0.1:6379",
		"http://127.1.2.3/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.10/",
		"http://172.16.0.1/",
		"http://100.64.0.1/",
		"http://0.0.0.0/",
		"http://[::1]/",
		"http://[fd00::1]/",
		"http://[fe80::1]/",
		"http://metadata.google.internal/",
		"not-a-url",
		"//example.com/no-scheme",
	}
	for _, raw := range cases {
		if _, err := Validate(context.Background(), raw); err == nil {
			t.Errorf("Validate(%q) = nil error, want rejection", raw)
		}
	}
}

func TestValidateAcceptsPublicIPs(t *testing.T) {
	for _, raw := range []string{
		"http://93.184.216.34/",
		"https://93.184.216.34:8443/path?q=1",
		"http://[2606:2800:220:1:248:1893:25c8:1946]/",
	} {
		if _, err := Validate(context.Background(), raw); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", raw, err)
		}
	}
}

func TestIsDisallowedIP(t *testing.T) {
	disallowed := []string{
		"127.0.0.1", "10.1.2.3", "192.168.0.1", "169.254.169.254",
		"100.64.0.1", "224.0.0.1", "::1", "fe80::1", "fc00::1", "::",
	}
	for _, raw := range disallowed {
		if !isDisallowedIP(net.ParseIP(raw)) {
			t.Errorf("isDisallowedIP(%s) = false, want true", raw)
		}
	}

	allowed := []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"}
	for _, raw := range allowed {
		if isDisallowedIP(net.ParseIP(raw)) {
			t.Errorf("isDisallowedIP(%s) = true, want false", raw)
		}
	}
}
