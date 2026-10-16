package publish

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
)

// maxGzipBytes is the published-size budget at ~4,000 servers (SPEC §8 Published data).
// A failure means the servers.json shape needs a decision (SPEC §8, bump v), not a bigger limit.
const maxGzipBytes = 100 * 1024

// sizeServers generates n servers shaped like the recorded upstream snapshot: 6-character
// join codes, official and community names, provider region and map IDs. Community names
// are random words so they compress no better than real ones.
func sizeServers(rng *rand.Rand, n int) []source.Server {
	regions := []string{"na-north", "na-south", "na-west", "eu-west", "eu-central", "eu-north", "asia-east", "asia-south", "oceania", "sa-east"}
	maps := []struct{ variant, base string }{
		{"Madrid", "Kavkazi"}, {"Harbor", "Kavkazi"}, {"Quarry", "Altis"}, {"Dunes", "Sahra"},
		{"Outpost", "Sahra"}, {"Valley", "Altis"}, {"Docks", "Port"}, {"Ridge", "Taiga"},
	}
	modes := []string{"KOTH_01", "CONQ_01", "CONQ_02", "TDM_01", "BREACH_01"}
	rulesets := []string{"standard", "hardcore", "no-vehicles", "pistols-only", "event"}
	words := []string{"Wolf", "Pack", "Iron", "Brigade", "Night", "Ops", "Euro", "Vets", "Casual", "Fun", "Elite", "Squad", "Hardcore", "Rangers", "Clan", "Server", "Official", "Training", "Weekend", "Warriors"}
	const codeChars = "abcdefghijklmnopqrstuvwxyz0123456789"

	servers := make([]source.Server, n)
	for i := range servers {
		code := make([]byte, 6)
		for j := range code {
			code[j] = codeChars[rng.Intn(len(codeChars))]
		}
		s := source.Server{
			ID:         string(code),
			InstanceID: fmt.Sprintf("inst-%d", i),
			Region:     regions[rng.Intn(len(regions))],
			MaxPlayers: []int{64, 100, 128}[rng.Intn(3)],
		}
		if i%4 == 0 {
			s.Type = source.Official
			s.Name = fmt.Sprintf("Official #%d", i)
			s.Rulesets = []string{"standard"}
		} else {
			s.Type = source.Community
			name := make([]string, 2+rng.Intn(4))
			for j := range name {
				name[j] = words[rng.Intn(len(words))]
			}
			s.Name = strings.Join(name, " ") + fmt.Sprintf(" %d", rng.Intn(1000))
			s.PasswordProtected = rng.Intn(10) == 0
			for _, r := range rulesets {
				if rng.Intn(4) == 0 {
					s.Rulesets = append(s.Rulesets, r)
				}
			}
		}
		if rng.Intn(5) > 0 {
			s.Players = 1 + rng.Intn(s.MaxPlayers)
		}
		m := maps[rng.Intn(len(maps))]
		mode := modes[rng.Intn(len(modes))]
		s.Map = source.Map{Variant: m.variant, Base: m.base, Name: m.variant}
		s.Mode = source.Mode{Experience: m.variant + "_" + mode, Name: mode}
		servers[i] = s
	}
	return servers
}

func TestGzipSizeAt4000Servers(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	servers := sizeServers(rng, 4000)
	tr := newTracker()
	s := snap("s1", t0, servers...)
	tr.Observe(s)
	// Most non-empty servers get a distinct exact start, so ms values don't repeat.
	for _, srv := range servers {
		if srv.Players > 0 && rng.Intn(10) < 8 {
			at := t0.Add(-time.Duration(rng.Intn(3600)) * time.Second)
			tr.Apply(srv.ID, source.MatchStart{State: source.Exact, At: at}, t0)
		}
	}

	dir := t.TempDir()
	if err := Write(context.Background(), dir, Build(s, tr, t0, pollDelay)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("servers.json at %d servers: %d bytes raw, %d bytes gzip", len(servers), len(raw), buf.Len())
	if buf.Len() >= maxGzipBytes {
		t.Errorf("gzip size = %d bytes, want < %d", buf.Len(), maxGzipBytes)
	}
}
