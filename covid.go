package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/google/go-github/v30/github"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/guptarohit/asciigraph"
	"github.com/spf13/pflag"
	"golang.org/x/text/message"
)

const remoteServerTimeout = 10 * time.Second

const (
	confirmed = iota
	dead
	recovered
)

type remoteData struct {
	repoContent *github.RepositoryContent
	response    *github.Response
}

type record struct {
	province string
	country  string
	lat      float32
	long     float32
	cases    []int
}

type data struct {
	header  []string
	records []record
}

var paths = [3]string{
	"time_series_covid19_confirmed_global.csv",
	"time_series_covid19_deaths_global.csv",
	"time_series_covid19_recovered_global.csv",
}

var (
	p  = message.NewPrinter(message.MatchLanguage("en"))
	w  = new(tabwriter.Writer)
	cl *github.Client
)

func main() {
	pflag.Usage = func() {
		fmt.Fprint(os.Stdout, `Shows number of COVID-19 cases.

Usage:
  covid [flags]

Flags:
  -e, --cache            enable request caching (default true)
  -c, --country string   country to show number of cases for
  -g  --graph            plot graph, only if country is selected
  -s, --save             save/overwrite data in file (default true)
  -t, --top-confirmed    Top 10 countries by most confirmed cases
      --top-dead         Top 10 countries by most dead cases
      --top-recovered    Top 10 countries by most recovered cases
  -v, --verbose          more verbose operation information
  -h, --help             help for covid
`)
	}

	var (
		fcache, fsave, ftopc, ftopd, ftopr, fgraph, fverbose, fhelp bool
		fcountry                                                    string
	)

	pflag.BoolVarP(&fcache, "cache", "e", true, "enable request caching")
	pflag.BoolVarP(&fsave, "save", "s", true, "save/overwrite data in file")
	pflag.BoolVarP(&ftopc, "top-confirmed", "t", false, "Top 10 countries by most confirmed cases")
	pflag.BoolVarP(&ftopd, "top-dead", "", false, "Top 10 countries by most dead cases")
	pflag.BoolVarP(&ftopr, "top-recovered", "", false, "Top 10 countries by most recovered cases")
	pflag.StringVarP(&fcountry, "country", "c", "", "country to show number of cases for")
	pflag.BoolVarP(&fgraph, "graph", "g", false, "plot graph, only if country is selected")
	pflag.BoolVarP(&fverbose, "verbose", "v", false, "more verbose operation information")
	pflag.BoolVarP(&fhelp, "help", "h", false, "help for covid")

	pflag.Parse()

	if fhelp {
		pflag.Usage()
		os.Exit(0)
	}

	if !fverbose {
		log.SetOutput(ioutil.Discard)
	}

	var wd string
	if home, err := os.UserHomeDir(); err != nil {
		log.Println("Could not get the user home dir")
	} else {
		wd = filepath.Join(home, "covid")
		if err := os.MkdirAll(filepath.Dir(wd), 0755); err != nil {
			log.Printf("Could not create working dir: %v\n", wd)
		}
	}

	var c *http.Client
	if fcache {
		c = httpcache.NewTransport(diskcache.New(filepath.Join(wd, "cache"))).Client()
	}
	cl = github.NewClient(c)

	var wg sync.WaitGroup
	var ci [3]chan remoteData
	var co [3]chan data
	for i := 0; i < 3; i++ {
		ci[i] = make(chan remoteData, 1)
		go getRemote(context.Background(), paths[i], ci[i])
		wg.Add(1)
		co[i] = make(chan data, 1)
		go decode(path.Join(wd, paths[i]), ci[i], co[i], &wg, fsave)
	}
	wg.Wait()

	conf := <-co[confirmed]
	dead := <-co[dead]
	recov := <-co[recovered]

	var (
		bold   = color.New(color.Bold).SprintFunc()
		green  = color.New(color.FgGreen).SprintFunc()
		yellow = color.New(color.FgYellow).SprintFunc()
		red    = color.New(color.FgRed).SprintFunc()
	)

	fmt.Printf("%v\n", bold("Globe"))
	w.Init(os.Stdout, 0, 0, 0, ' ', 0)
	conf.printCases("Confirmed", yellow)
	dead.printCases("Dead", red)
	recov.printCases("Recovered", green)
	w.Flush()

	if fcountry != "" {
		cconf, found := conf.filter(fcountry)
		if !found {
			fmt.Fprintf(os.Stderr, "\nCountry %v is not in the list\n", bold(fcountry))
			os.Exit(1)
		}

		cdead, found := dead.filter(fcountry)
		if !found {
			fmt.Fprintf(os.Stderr, "\nCountry %v is not in the list\n", bold(fcountry))
			os.Exit(1)
		}

		crecov, found := recov.filter(fcountry)
		if !found {
			fmt.Fprintf(os.Stderr, "\nCountry %v is not in the list\n", bold(fcountry))
			os.Exit(1)
		}

		fmt.Printf("\n%v\n", bold(cconf.country))
		w.Init(os.Stdout, 0, 0, 0, ' ', 0)
		cconf.printCases("Confirmed", yellow)
		cdead.printCases("Dead", red)
		crecov.printCases("Recovered", green)
		w.Flush()

		if fgraph {
			cconf.printGraph("Confirmed", yellow)
			cdead.printGraph("Dead", red)
			crecov.printGraph("Recovered", green)
		}
	}

	if ftopc {
		rconf := conf.reduce()
		rconf.sort()
		fmt.Printf("\n%v\n", bold("Top 10 countries by most confirmed cases"))
		w.Init(os.Stdout, 20, 0, 0, '.', 0)
		for i := 0; i < 10; i++ {
			fmt.Fprintf(w, "%2v-%v\t%v\n", i+1, rconf.records[i].country, yellow(rconf.records[i].cases[len(rconf.records[i].cases)-1]))
		}
		w.Flush()
	}

	if ftopd {
		rdead := dead.reduce()
		rdead.sort()
		fmt.Printf("\n%v\n", bold("Top 10 countries by most dead cases"))
		w.Init(os.Stdout, 20, 0, 0, '.', 0)
		for i := 0; i < 10; i++ {
			fmt.Fprintf(w, "%2v-%v\t%v\n", i+1, rdead.records[i].country, red(rdead.records[i].cases[len(rdead.records[i].cases)-1]))
		}
		w.Flush()
	}

	if ftopr {
		rrecov := recov.reduce()
		rrecov.sort()
		fmt.Printf("\n%v\n", bold("Top 10 countries by most recovered cases"))
		w.Init(os.Stdout, 20, 0, 0, '.', 0)
		for i := 0; i < 10; i++ {
			fmt.Fprintf(w, "%2v-%v\t%v\n", i+1, rrecov.records[i].country, green(rrecov.records[i].cases[len(rrecov.records[i].cases)-1]))
		}
		w.Flush()
	}
}

func getRemote(ctx context.Context, path string, ch chan<- remoteData) {
	defer close(ch)

	log.Printf("Get remote data, path: %v", path)
	ctx, cancel := context.WithTimeout(ctx, remoteServerTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "Cannot get data: %v\n", ctx.Err())
		os.Exit(1)
	default:
		repContent, _, resp, err := cl.Repositories.GetContents(ctx, "CSSEGISandData", "COVID-19", fmt.Sprintf("csse_covid_19_data/csse_covid_19_time_series/%s", path), nil)
		if err != nil {
			if _, ok := err.(*github.RateLimitError); ok {
				fmt.Fprintf(os.Stderr, "hit rate limit: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "cannot get data: %v\n", err)
			os.Exit(1)
		}
		log.Printf("Response received: %v", resp)
		ch <- remoteData{repContent, resp}
	}
}

func decode(path string, ci <-chan remoteData, co chan<- data, wg *sync.WaitGroup, save bool) {
	defer close(co)

	rd := <-ci
	log.Printf("Convert data, path: %v", path)
	c, err := base64.StdEncoding.DecodeString(*rd.repoContent.Content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot decode data: %v\n", err)
		os.Exit(1)
	}
	content := string(c)

	if save {
		f, err := os.Create(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot create file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		if _, err = io.WriteString(f, content); err != nil {
			fmt.Fprintf(os.Stderr, "Cannot write to file: %v\n", err)
			os.Exit(1)
		}
		f.Sync()
	}

	r := csv.NewReader(strings.NewReader(content))
	rr, err := r.ReadAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read csv data: %v\n", err)
		os.Exit(1)
	}

	var recs []record
	for i := 1; i < len(rr); i++ {
		var cases []int
		for j := 4; j < len(rr[0]); j++ {
			n, err := strconv.Atoi(rr[i][j])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot convert number: %v\n", err)
				os.Exit(1)
			}
			cases = append(cases, n)
		}
		recs = append(recs, record{rr[i][0], rr[i][1], atof(rr[i][2]), atof(rr[i][3]), cases})
	}

	wg.Done()
	co <- data{rr[0], recs}
}

func (d data) filter(c string) (record, bool) {
	d.reduce()
	for i := 0; i < len(d.records); i++ {
		if strings.EqualFold(d.records[i].country, c) {
			return d.records[i], true
		}
	}
	return record{}, false
}

func (d data) reduce() data {
	d.sortCountry()
	var rs []record
	c := ""
	for i := 0; i < len(d.records); i++ {
		if d.records[i].country != c {
			rs = append(rs, d.records[i])
			c = d.records[i].country
		} else {
			l := len(rs) - 1
			for j := 0; j < len(d.records[i].cases); j++ {
				rs[l].cases[j] += d.records[i].cases[j]
			}
		}
	}
	return data{d.header, rs}
}

func (d data) sum(c int) int {
	if c < 0 {
		c = len(d.records[0].cases) + c
	}
	s := 0
	for _, r := range d.records {
		s += r.cases[c]
	}
	return s
}

func (d data) printCases(t string, c func(a ...interface{}) string) {
	s := d.sum(-1)
	n := s - d.sum(-2)
	fmt.Fprintf(w, "%v: \t%v \tNew: %v\n", t, c(p.Sprint(s)), c(p.Sprint(n)))
}

func (d data) sort() {
	sort.Slice(d.records, func(i, j int) bool {
		return d.records[i].cases[len(d.records[0].cases)-1] > d.records[j].cases[len(d.records[0].cases)-1]
	})
}

func (d data) sortCountry() {
	sort.Slice(d.records, func(i, j int) bool {
		return d.records[i].country < d.records[j].country
	})
}

func (r record) printCases(t string, c func(a ...interface{}) string) {
	s := r.cases[len(r.cases)-1]
	n := s - r.cases[len(r.cases)-2]
	fmt.Fprintf(w, "%v: \t%v \tNew: %v\n", t, c(p.Sprint(s)), c(p.Sprint(n)))
}

func (r record) printGraph(t string, c func(a ...interface{}) string) {
	var ff []float64
	for _, n := range r.cases {
		ff = append(ff, float64(n))
	}
	fmt.Println()
	fmt.Println(c(asciigraph.Plot(ff, asciigraph.Caption(r.country+" - "+t), asciigraph.Width(70), asciigraph.Height(20))))

}

func atof(s string) float32 {
	n, err := strconv.ParseFloat(s, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot convert number: %v\n", err)
		os.Exit(1)
	}
	return float32(n)
}
