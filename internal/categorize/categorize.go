// Package categorize buckets listeners into human-meaningful groups —
// the "what kind of thing is squatting on this port" signal.
package categorize

import "strings"

// Category is a coarse classification of a listening process.
type Category string

const (
	Database    Category = "database"
	WebServer   Category = "web"
	Development Category = "dev"
	Messaging   Category = "messaging"
	Tunnel      Category = "tunnel"
	System      Category = "system"
	Other       Category = "other"
)

// All lists every category in display order.
var All = []Category{Development, WebServer, Database, Messaging, Tunnel, System, Other}

// Badge returns a short display label.
func (c Category) Badge() string {
	switch c {
	case Database:
		return "DB"
	case WebServer:
		return "WEB"
	case Development:
		return "DEV"
	case Messaging:
		return "MSG"
	case Tunnel:
		return "TUN"
	case System:
		return "SYS"
	default:
		return "·"
	}
}

// Rank orders categories for grouped display — the things a developer is
// usually hunting for come first.
func (c Category) Rank() int {
	switch c {
	case Development:
		return 0
	case WebServer:
		return 1
	case Database:
		return 2
	case Messaging:
		return 3
	case Tunnel:
		return 4
	case System:
		return 5
	default:
		return 6
	}
}

// Noise reports whether the category is background noise (system daemons,
// unclassified) rather than something a developer typically came to find.
func (c Category) Noise() bool { return c == System || c == Other }

// Title is the human-friendly group heading for the category.
func (c Category) Title() string {
	switch c {
	case Development:
		return "development servers"
	case WebServer:
		return "web servers & proxies"
	case Database:
		return "databases & caches"
	case Messaging:
		return "messaging & queues"
	case Tunnel:
		return "tunnels & forwards"
	case System:
		return "system daemons"
	default:
		return "uncategorized"
	}
}

// Parse maps a user-supplied string (flag value) to a Category.
func Parse(s string) (Category, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "database", "db":
		return Database, true
	case "web", "webserver", "web-server":
		return WebServer, true
	case "dev", "development":
		return Development, true
	case "messaging", "msg", "queue":
		return Messaging, true
	case "tunnel", "tun":
		return Tunnel, true
	case "system", "sys":
		return System, true
	case "other":
		return Other, true
	}
	return Other, false
}

// nameRules match against the lowercase process name (substring). They take
// precedence over port numbers — a postgres on a non-standard port is still
// a database.
var nameRules = []struct {
	substr string
	cat    Category
}{
	// databases & caches
	{"postgres", Database}, {"mysqld", Database}, {"mariadb", Database},
	{"redis", Database}, {"valkey", Database}, {"mongod", Database},
	{"clickhouse", Database}, {"elasticsearch", Database}, {"opensearch", Database},
	{"cockroach", Database}, {"memcached", Database}, {"influxd", Database},
	{"meilisearch", Database}, {"typesense", Database},

	// message brokers & queues
	{"rabbitmq", Messaging}, {"kafka", Messaging}, {"nats-server", Messaging},
	{"mosquitto", Messaging}, {"pulsar", Messaging}, {"beam.smp", Messaging},

	// web servers & proxies
	{"nginx", WebServer}, {"caddy", WebServer}, {"httpd", WebServer},
	{"apache", WebServer}, {"traefik", WebServer}, {"haproxy", WebServer},
	{"envoy", WebServer},

	// development runtimes & tooling
	{"node", Development}, {"bun", Development}, {"deno", Development},
	{"vite", Development}, {"webpack", Development}, {"next", Development},
	{"turbopack", Development}, {"esbuild", Development},
	{"python", Development}, {"uvicorn", Development}, {"gunicorn", Development},
	{"flask", Development}, {"django", Development},
	{"ruby", Development}, {"puma", Development}, {"rails", Development},
	{"php", Development}, {"java", Development}, {"gradle", Development},
	{"cargo", Development}, {"go run", Development}, {"air", Development},
	{"dlv", Development}, {"workerd", Development}, {"wrangler", Development},
	{"miniflare", Development}, {"inngest", Development}, {"ngrok", Development},
	{"docker", Development}, {"com.docker", Development}, {"colima", Development},
	{"containerd", Development}, {"kubectl", Development}, {"tilt", Development},
	{"mitmproxy", Development}, {"mitmweb", Development}, {"jest", Development},
	{"playwright", Development}, {"storybook", Development}, {"ollama", Development},

	// tunnels & forwarders (ssh client is handled as a carrier in Categorize)
	{"cloudflared", Tunnel},

	// system daemons (macOS + linux staples)
	{"sshd", System}, {"launchd", System},
	{"systemd", System}, {"controlce", System}, {"rapportd", System},
	{"sharingd", System}, {"airplay", System}, {"mediasharingd", System},
	{"cupsd", System}, {"bluetooth", System}, {"mdnsresponder", System},
	{"tailscaled", System}, {"setapp", System}, {"dropbox", System},
	{"onedrive", System}, {"spotify", System},
}

// portRules cover well-known ports, used when no name rule matched.
var portRules = map[uint32]Category{
	// web
	80: WebServer, 443: WebServer, 8443: WebServer,
	// databases
	3306: Database, 5432: Database, 6379: Database, 27017: Database,
	9200: Database, 9300: Database, 8529: Database, 7474: Database,
	7687: Database, 5984: Database, 8086: Database, 9042: Database,
	1521: Database, 1433: Database, 26257: Database, 8123: Database,
	11211: Database,
	// messaging
	5672: Messaging, 15672: Messaging, 9092: Messaging, 4222: Messaging,
	1883: Messaging, 61616: Messaging, 4150: Messaging,
	// dev servers
	3000: Development, 3001: Development, 4200: Development, 5173: Development,
	5174: Development, 8000: Development, 8080: Development, 8081: Development,
	8888: Development, 9229: Development, 6006: Development, 1313: Development,
	4321: Development, 19000: Development, 19006: Development,
	8787: Development, 8788: Development, 8288: Development, 3333: Development,
	// system
	22: System, 53: System, 88: System, 445: System, 548: System,
	631: System, 3689: System, 5000: System, 5900: System, 7000: System,
}

// carriers forward traffic for something else — their name says nothing about
// what the port is for, so the port rule wins (an ssh -L of your dev server is
// a dev port, not a system daemon). Unmatched carried ports are tunnels.
var carriers = map[string]bool{"ssh": true, "autossh": true}

// IsCarrier reports whether the process is a traffic carrier (ssh forward) —
// the port is served by another machine and only relayed here.
func IsCarrier(name string) bool { return carriers[strings.ToLower(name)] }

// Categorize classifies a listener from its process name, cmdline, and port.
func Categorize(port uint32, name, cmdline string) Category {
	hay := strings.ToLower(name)
	if carriers[hay] {
		if c, ok := portRules[port]; ok {
			return c
		}
		return Tunnel
	}
	for _, r := range nameRules {
		if strings.Contains(hay, r.substr) {
			return r.cat
		}
	}
	// A second pass over the cmdline catches interpreters running a known
	// tool (e.g. "node /…/vite/bin/vite.js" when the name is just "node" —
	// already dev — but also "python -m mitmproxy" style invocations).
	hay = strings.ToLower(cmdline)
	for _, r := range nameRules {
		if strings.Contains(hay, r.substr) {
			return r.cat
		}
	}
	if c, ok := portRules[port]; ok {
		return c
	}
	return Other
}
