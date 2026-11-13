package api

import "testing"

func ptr(v int) *int { return &v }

func TestScrapeRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     ScrapeRequest
		wantErr bool
	}{
		{"defaults", ScrapeRequest{}, false},
		{"limit min", ScrapeRequest{Limit: ptr(1)}, false},
		{"limit max", ScrapeRequest{Limit: ptr(MaxPageLimit)}, false},
		{"limit zero", ScrapeRequest{Limit: ptr(0)}, true},
		{"limit negative", ScrapeRequest{Limit: ptr(-5)}, true},
		{"limit too large", ScrapeRequest{Limit: ptr(MaxPageLimit + 1)}, true},
		{"wait min", ScrapeRequest{WaitSeconds: ptr(0)}, false},
		{"wait max", ScrapeRequest{WaitSeconds: ptr(MaxWaitSeconds)}, false},
		{"wait negative", ScrapeRequest{WaitSeconds: ptr(-1)}, true},
		{"wait too large", ScrapeRequest{WaitSeconds: ptr(MaxWaitSeconds + 1)}, true},
	}
	for _, tc := range cases {
		err := tc.req.validate()
		if tc.wantErr && err == nil {
			t.Errorf("%s: validate() = nil, want error", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: validate() = %v, want nil", tc.name, err)
		}
	}
}
