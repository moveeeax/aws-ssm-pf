package fuzzy

import "testing"

func TestScoreSubsequence(t *testing.T) {
	if _, ok := Score("web", "web-server-01"); !ok {
		t.Fatal("expected match")
	}
	if _, ok := Score("xyz", "web-server-01"); ok {
		t.Fatal("expected no match")
	}
	if _, ok := Score("", "anything"); !ok {
		t.Fatal("empty query should match")
	}
}

func TestScoreRanksPrefixHigher(t *testing.T) {
	contig, _ := Score("web", "web-01")
	scattered, _ := Score("web", "wxexbx-99")
	if contig <= scattered {
		t.Errorf("contiguous/boundary match (%d) should beat scattered (%d)", contig, scattered)
	}
}

func TestRankOrder(t *testing.T) {
	items := []string{"cache-prod", "web-01", "webhook-worker", "db-primary"}
	got := Rank(items, "web", func(s string) string { return s })
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %v", got)
	}
	if got[0] != "web-01" {
		t.Errorf("want web-01 first (shorter boundary match), got %q", got[0])
	}
}

func TestRankEmptyQueryReturnsAll(t *testing.T) {
	items := []string{"a", "b", "c"}
	got := Rank(items, "  ", func(s string) string { return s })
	if len(got) != 3 {
		t.Fatalf("empty query should return all, got %v", got)
	}
}
