package compose

import (
	"reflect"
	"testing"
)

const ndjson = `{"Project":"Shop","Service":"web","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":80,"PublishedPort":8080,"Protocol":"tcp"},{"URL":"::","TargetPort":80,"PublishedPort":8080,"Protocol":"tcp"}]}
{"Project":"Shop","Service":"db","State":"running","Publishers":[{"URL":"127.0.0.1","TargetPort":5432,"PublishedPort":5433,"Protocol":"tcp"}]}
{"Project":"Shop","Service":"worker","State":"running","Publishers":[]}
{"Project":"Shop","Service":"dns","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":53,"PublishedPort":5353,"Protocol":"udp"}]}
{"Project":"Shop","Service":"lan","State":"running","Publishers":[{"URL":"192.168.1.5","TargetPort":80,"PublishedPort":9000,"Protocol":"tcp"}]}
{"Project":"Shop","Service":"old","State":"exited","Publishers":[{"URL":"0.0.0.0","TargetPort":80,"PublishedPort":9001,"Protocol":"tcp"}]}
{"Project":"Shop","Service":"web","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":80,"PublishedPort":8081,"Protocol":"tcp"}]}
`

func TestParsePS(t *testing.T) {
	want := []Service{
		{Project: "Shop", Service: "db", Ports: []int{5433}},
		{Project: "Shop", Service: "web", Ports: []int{8080, 8081}}, // replicas merged
	}
	got, err := ParsePS([]byte(ndjson))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("NDJSON: got %+v %v", got, err)
	}
	array := `[{"Project":"shop","Service":"api","State":"running","Publishers":[{"URL":"","TargetPort":3000,"PublishedPort":3000,"Protocol":"tcp"}]}]`
	got, err = ParsePS([]byte(array))
	if err != nil || len(got) != 1 || got[0].Ports[0] != 3000 {
		t.Fatalf("array: %+v %v", got, err)
	}
	if got, err := ParsePS([]byte("\n")); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	if _, err := ParsePS([]byte("not json")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestHostname(t *testing.T) {
	if h := (Service{Project: "My_Shop", Service: "web_ui"}).Hostname("localhost"); h != "web-ui.my-shop.localhost" {
		t.Fatal(h)
	}
}

func TestSplitArgs(t *testing.T) {
	for _, tc := range []struct{ in, global, rest []string }{
		{nil, nil, []string{"up"}},
		{[]string{"up", "-d"}, nil, []string{"up", "-d"}},
		{[]string{"-f", "dev.yml", "-p", "x", "up", "--build"}, []string{"-f", "dev.yml", "-p", "x"}, []string{"up", "--build"}},
		{[]string{"--file=dev.yml", "watch"}, []string{"--file=dev.yml"}, []string{"watch"}},
		{[]string{"-f", "dev.yml"}, []string{"-f", "dev.yml"}, []string{"up"}},
	} {
		g, r := SplitArgs(tc.in)
		if !reflect.DeepEqual(g, tc.global) || !reflect.DeepEqual(r, tc.rest) {
			t.Errorf("SplitArgs(%q) = %q %q, want %q %q", tc.in, g, r, tc.global, tc.rest)
		}
	}
	if !Detached([]string{"up", "--build", "-d"}) || Detached([]string{"up"}) || Detached([]string{"logs", "-d"}) {
		t.Error("Detached")
	}
}
