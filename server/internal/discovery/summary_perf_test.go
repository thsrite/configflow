package discovery

import (
	"fmt"
	"reflect"
	"testing"
)

func BenchmarkSummarizeRepeatedHosts(b *testing.B) {
	hosts := make(trafficRows[trafficHost], 500)
	for i := range hosts {
		hosts[i] = trafficRow[trafficHost]{fmt.Sprintf("h%d.d%d.example.com", i, i%100), trafficHost{Routes: trafficRows[Stats]{{"Match\t\tdirect\tDIRECT", Stats{Conns: 1}}}}}
	}
	report := traffic{}
	for i := range 7 {
		report.Days = append(report.Days, trafficRow[trafficDay]{now().AddDate(0, 0, -i).Format("2006-01-02"), trafficDay{Hosts: hosts}})
	}
	reports := []struct {
		agent Agent
		data  traffic
	}{{Agent{ID: "a"}, report}, {Agent{ID: "b"}, report}}
	b.ReportAllocs()
	for b.Loop() {
		summarize(reports, 7, "uncovered", "", nil, nil)
	}
}

func TestRepeatedHostAcrossDaysMatchesCombinedTraffic(t *testing.T) {
	route := func(n int64) trafficRows[trafficHost] {
		return trafficRows[trafficHost]{{"one.example.com", trafficHost{Routes: trafficRows[Stats]{{"Match\t\tdirect\tDIRECT", Stats{Conns: n}}}}}}
	}
	list := func(days trafficRows[trafficDay]) Listing {
		return summarize([]struct {
			agent Agent
			data  traffic
		}{{Agent{ID: "a"}, traffic{Days: days}}}, 7, "all", "", nil, nil)
	}
	today, yesterday := now().Format("2006-01-02"), now().AddDate(0, 0, -1).Format("2006-01-02")
	got := list(trafficRows[trafficDay]{{yesterday, trafficDay{Hosts: route(2)}}, {today, trafficDay{Hosts: route(3)}}})
	want := list(trafficRows[trafficDay]{{today, trafficDay{Hosts: route(5)}}})
	if !reflect.DeepEqual(got, want) || len(got.Items) != 1 || got.Items[0].Domain != "example.com" || got.Items[0].Conns != 5 {
		t.Fatalf("combined traffic differs: %+v, %+v", got, want)
	}
}
