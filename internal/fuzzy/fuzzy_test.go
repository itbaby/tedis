package fuzzy

import (
	"reflect"
	"testing"
)

func TestMatchSubsequence(t *testing.T) {
	if _, ok := Match("djs", "demo:str:json"); !ok {
		t.Fatal("djs should match demo:str:json")
	}
	if _, ok := Match("djs", "demo:str:php"); ok {
		t.Fatal("djs must not match demo:str:php")
	}
	if _, ok := Match("ABC", "abc:def"); !ok {
		t.Fatal("case-insensitive")
	}
	if sc, ok := Match("", "anything"); !ok || sc != 0 {
		t.Fatal("empty pattern matches all")
	}
}

func TestScoreOrdering(t *testing.T) {
	// prefix-ish and boundary hits beat scattered interior hits
	start, _ := Match("user", "user:1001")
	mid, _ := Match("user", "session:user:a")
	if start <= mid {
		t.Fatalf("start=%d mid=%d", start, mid)
	}
	// consecutive runs beat broken runs
	run, _ := Match("ada", "ada")
	broken, _ := Match("ada", "a-d-a")
	if run <= broken {
		t.Fatalf("run=%d broken=%d", run, broken)
	}
}

func TestFilterRanking(t *testing.T) {
	got := Filter("djs", []string{
		"demo:str:php",  // no 'j': must not match
		"demo:str:json", // boundary hits d…j…s
		"users:x",       // no d/j/s sequence
		"demo:json:sub", // scattered d..j..s
	})
	want := []string{"demo:json:sub", "demo:str:json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
