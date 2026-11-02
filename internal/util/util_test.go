package util

import "testing"

func TestHumanSize(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_NUMERIC", "")
	t.Setenv("LANG", "")
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1126, "1.1KB"},
		{1177, "1.2KB"},
		{15360, "15KB"},
		{15447, "16KB"},
		{54360, "54KB"},
		{10230, "10KB"},
		{10240, "10KB"},
		{10241, "11KB"},
		{1024 * 1024, "1.0MB"},
		{1024 * 1024 * 1024, "1.0GB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.n); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestHumanSizeLocale(t *testing.T) {
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	if got := HumanSize(3584); got != "3,5KB" {
		t.Errorf("locale comma: got %q", got)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_NUMERIC", "de_DE.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := HumanSize(3584); got != "3,5KB" {
		t.Errorf("LC_NUMERIC comma: got %q", got)
	}
}

func TestFloat(t *testing.T) {
	cases := map[float64]string{95: "95.0", 4.4: "4.4", 95.017503: "95.017503", 0: "0.0"}
	for in, want := range cases {
		if got := Float(in); got != want {
			t.Errorf("Float(%v) = %q, want %q", in, got, want)
		}
	}
}
