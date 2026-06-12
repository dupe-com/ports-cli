package categorize

import "testing"

func TestCategorize(t *testing.T) {
	cases := []struct {
		port    uint32
		name    string
		cmdline string
		want    Category
	}{
		// name rules beat port rules
		{8080, "postgres", "postgres -D /data", Database},
		{12345, "redis-server", "redis-server *:12345", Database},
		{80, "nginx", "nginx: master process", WebServer},
		{3000, "node", "node server.js", Development},
		{3003, "bun", "bun src/index.ts", Development},
		// cmdline second pass
		{4567, "MyApp", "node /x/vite/bin/vite.js", Development},
		// port fallback
		{5432, "mystery", "/opt/mystery", Database},
		{443, "mystery", "/opt/mystery", WebServer},
		{9092, "mystery", "/opt/mystery", Messaging},
		{22, "mystery", "/opt/mystery", System},
		// nothing known
		{45678, "mystery", "/opt/mystery", Other},
		// macOS daemons
		{7000, "ControlCenter", "/System/.../ControlCenter", System},
		{11434, "ollama", "ollama serve", Development},
	}
	for _, c := range cases {
		if got := Categorize(c.port, c.name, c.cmdline); got != c.want {
			t.Errorf("Categorize(%d, %q) = %s, want %s", c.port, c.name, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Category
		ok   bool
	}{
		{"db", Database, true},
		{"DATABASE", Database, true},
		{"web", WebServer, true},
		{"dev", Development, true},
		{"msg", Messaging, true},
		{"sys", System, true},
		{"other", Other, true},
		{"bogus", Other, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Parse(%q) = (%s, %v), want (%s, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestBadges(t *testing.T) {
	for _, c := range All {
		if c.Badge() == "" {
			t.Errorf("category %s has empty badge", c)
		}
	}
}

func TestRankOrdersMainCategoriesFirst(t *testing.T) {
	order := []Category{Development, WebServer, Database, Messaging, System, Other}
	for i := 1; i < len(order); i++ {
		if order[i-1].Rank() >= order[i].Rank() {
			t.Errorf("Rank(%s)=%d not < Rank(%s)=%d", order[i-1], order[i-1].Rank(), order[i], order[i].Rank())
		}
	}
}

func TestNoise(t *testing.T) {
	for _, c := range []Category{Development, WebServer, Database, Messaging} {
		if c.Noise() {
			t.Errorf("%s should not be noise", c)
		}
	}
	for _, c := range []Category{System, Other} {
		if !c.Noise() {
			t.Errorf("%s should be noise", c)
		}
	}
}
