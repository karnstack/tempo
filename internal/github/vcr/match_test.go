package vcr

import (
	"net/url"
	"testing"
)

func TestCanonicalBodyJSONEquivalent(t *testing.T) {
	a := []byte(`{"query":"{viewer{login}}","variables":{"owner":"octocat","name":"hello"}}`)
	b := []byte("{\n  \"variables\":{ \"name\":\"hello\", \"owner\":\"octocat\" },\n  \"query\":\"{viewer{login}}\"\n}")
	if canonicalBody(a) != canonicalBody(b) {
		t.Errorf("expected equivalent JSON to canonicalise to same string\n  a=%s\n  b=%s",
			canonicalBody(a), canonicalBody(b))
	}
}

func TestCanonicalBodyJSONDifferentVarsDontMatch(t *testing.T) {
	a := []byte(`{"variables":{"owner":"octocat"}}`)
	b := []byte(`{"variables":{"owner":"dependabot"}}`)
	if canonicalBody(a) == canonicalBody(b) {
		t.Error("expected different vars to canonicalise differently")
	}
}

func TestCanonicalBodyEmpty(t *testing.T) {
	if canonicalBody(nil) != "" {
		t.Error("nil body should canonicalise to empty string")
	}
	if canonicalBody([]byte{}) != "" {
		t.Error("empty body should canonicalise to empty string")
	}
}

func TestCanonicalBodyNonJSONPassThrough(t *testing.T) {
	in := []byte("not json")
	if canonicalBody(in) != "not json" {
		t.Errorf("non-JSON should pass through unchanged, got %q", canonicalBody(in))
	}
}

func TestCanonicalQuerySortKeys(t *testing.T) {
	a := url.Values{"z": []string{"1"}, "a": []string{"2"}}
	b := url.Values{"a": []string{"2"}, "z": []string{"1"}}
	if canonicalQuery(a) != canonicalQuery(b) {
		t.Error("query key order shouldn't matter")
	}
	if canonicalQuery(a) != "a=2&z=1" {
		t.Errorf("got %q", canonicalQuery(a))
	}
}

func TestCanonicalQueryPreservesValueOrder(t *testing.T) {
	q := url.Values{"page": []string{"1", "2"}}
	if got := canonicalQuery(q); got != "page=1&page=2" {
		t.Errorf("value order should be preserved, got %q", got)
	}
}

func TestMatchKeyHostIgnored(t *testing.T) {
	k1, err := matchKey("GET", "https://api.github.com/repos/x/y?per_page=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := matchKey("GET", "http://127.0.0.1:8080/repos/x/y?per_page=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Errorf("host should be ignored:\n  k1=%q\n  k2=%q", k1, k2)
	}
}

func TestMatchKeyDifferentMethodDiffers(t *testing.T) {
	k1, _ := matchKey("GET", "https://api.github.com/x", nil)
	k2, _ := matchKey("POST", "https://api.github.com/x", nil)
	if k1 == k2 {
		t.Error("different methods should produce different keys")
	}
}

func TestMatchKeyCaseInsensitiveMethod(t *testing.T) {
	k1, _ := matchKey("get", "https://api.github.com/x", nil)
	k2, _ := matchKey("GET", "https://api.github.com/x", nil)
	if k1 != k2 {
		t.Errorf("method should be normalised to uppercase")
	}
}

func TestMatchKeyGraphQLWhitespaceTolerant(t *testing.T) {
	a := []byte(`{"query":"{viewer{login}}","variables":{}}`)
	b := []byte("{\n\t\"query\":   \"{viewer{login}}\",\n\t\"variables\": {}\n}")
	k1, _ := matchKey("POST", "https://api.github.com/graphql", a)
	k2, _ := matchKey("POST", "https://api.github.com/graphql", b)
	if k1 != k2 {
		t.Errorf("GraphQL bodies with different whitespace should match:\n  k1=%q\n  k2=%q", k1, k2)
	}
}
