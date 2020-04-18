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
	"github.com/spf13/cobra"
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
	bold   = color.New(color.Bold).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()

	p = message.NewPrinter(message.MatchLanguage("en"))

	w = new(tabwriter.Writer)

	rootCmd = &cobra.Command{}

	argCountry string
	argCache   bool
	argSave    bool
	argTopc    bool
	argTopd    bool
	argTopr    bool
	argVerbose bool

	wd string

	cl *github.Client
)

func init() {
	rootCmd.Use = "covid"
	rootCmd.Short = "Shows number of COVID-19 cases."

	rootCmd.Flags().StringVarP(&argCountry, "country", "c", "", "country to show number of cases for")
	rootCmd.Flags().BoolVarP(&argCache, "cache", "e", true, "enable request caching")
	rootCmd.Flags().BoolVarP(&argSave, "save", "s", true, "save/overwrite data in file")
	rootCmd.Flags().BoolVarP(&argTopc, "top-confirmed", "t", false, "Top 10 countries by most confirmed cases")
	rootCmd.Flags().BoolVarP(&argTopd, "top-dead", "", false, "Top 10 countries by most dead cases")
	rootCmd.Flags().BoolVarP(&argTopr, "top-recovered", "", false, "Top 10 countries by most recovered cases")
	rootCmd.Flags().BoolVarP(&argVerbose, "verbose", "v", false, "more verbose operation information")
}

func main() {
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		if !argVerbose {
			log.SetOutput(ioutil.Discard)
		}

		if home, err := os.UserHomeDir(); err != nil {
			log.Println("Could not get the user home dir")
		} else {
			wd = home + string(os.PathSeparator) + "covid" + string(os.PathSeparator)
			if err := os.MkdirAll(filepath.Dir(wd), 0755); err != nil {
				log.Printf("Could not create working dir: %v\n", wd)
			}
		}

		var c *http.Client
		if argCache {
			c = &http.Client{Transport: httpcache.NewTransport(diskcache.New(wd + "cache"))}
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
			go convertAndSave(wd+paths[i], ci[i], co[i], &wg)
		}
		wg.Wait()

		conf := <-co[confirmed]
		dead := <-co[dead]
		recov := <-co[recovered]

		fmt.Printf("%v\n", bold("Globe"))
		w.Init(os.Stdout, 0, 0, 0, ' ', 0)
		conf.printCases("Confirmed", yellow)
		dead.printCases("Dead", red)
		recov.printCases("Recovered", green)
		w.Flush()

		if argCountry != "" {
			cconf, found := conf.filter(argCountry)
			if !found {
				fmt.Printf("\nCountry %v is not in the list\n", bold(argCountry))
				os.Exit(1)
			}

			cdead, found := dead.filter(argCountry)
			if !found {
				fmt.Printf("\nCountry %v is not in the list\n", bold(argCountry))
				os.Exit(1)
			}

			crecov, found := recov.filter(argCountry)
			if !found {
				fmt.Printf("\nCountry %v is not in the list\n", bold(argCountry))
				os.Exit(1)
			}

			fmt.Printf("\n%v\n", bold(cconf.records[0].country))
			w.Init(os.Stdout, 0, 0, 0, ' ', 0)
			cconf.printCases("Confirmed", yellow)
			cdead.printCases("Dead", red)
			crecov.printCases("Recovered", green)
			w.Flush()
		}

		if argTopc {
			conf.sort()
			fmt.Printf("\n%v\n", bold("Top 10 countries by most confirmed cases"))
			for i := 0; i < 10; i++ {
				fmt.Printf("%v\n", yellow(i+1, "-", conf.records[i].country))
			}
		}

		if argTopd {
			dead.sort()
			fmt.Printf("\n%v\n", bold("Top 10 countries by most dead cases"))
			for i := 0; i < 10; i++ {
				fmt.Printf("%v\n", red(i+1, "-", dead.records[i].country))
			}
		}

		if argTopr {
			recov.sort()
			fmt.Printf("\n%v\n", bold("Top 10 countries by most recovered cases"))
			for i := 0; i < 10; i++ {
				fmt.Printf("%v\n", green(i+1, "-", recov.records[i].country))
			}
		}
	}
	rootCmd.Execute()
}

func getRemote(ctx context.Context, path string, ch chan<- remoteData) {
	defer close(ch)

	log.Printf("Get remote data, path: %v", path)
	ctx, cancel := context.WithTimeout(ctx, remoteServerTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		fmt.Printf("Cannot get data: %v\n", ctx.Err())
		os.Exit(1)
	default:
		repContent, _, resp, err := cl.Repositories.GetContents(ctx, "CSSEGISandData", "COVID-19", fmt.Sprintf("csse_covid_19_data/csse_covid_19_time_series/%s", path), nil)
		if err != nil {
			if _, ok := err.(*github.RateLimitError); ok {
				fmt.Printf("hit rate limit: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("cannot get data: %v\n", err)
			os.Exit(1)
		}
		log.Printf("Response received: %v", resp)
		ch <- remoteData{repContent, resp}
	}
}

func convertAndSave(path string, ci <-chan remoteData, co chan<- data, wg *sync.WaitGroup) {
	defer close(co)

	rd := <-ci
	log.Printf("Convert data, path: %v", path)
	c, err := base64.StdEncoding.DecodeString(*rd.repoContent.Content)
	if err != nil {
		fmt.Printf("Cannot decode data: %v\n", err)
		os.Exit(1)
	}
	content := string(c)

	if argSave {
		f, err := os.Create(path)
		if err != nil {
			fmt.Printf("Cannot create file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		_, err = io.WriteString(f, content)
		if err != nil {
			fmt.Printf("Cannot write to file: %v\n", err)
			os.Exit(1)
		}
		f.Sync()
	}

	r := csv.NewReader(strings.NewReader(content))
	rr, err := r.ReadAll()
	if err != nil {
		fmt.Printf("Cannot read csv data: %v\n", err)
		os.Exit(1)
	}

	var recs []record
	for i := 1; i < len(rr); i++ {
		var cases []int
		for j := 4; j < len(rr[0]); j++ {
			n, err := strconv.Atoi(rr[i][j])
			if err != nil {
				fmt.Printf("Cannot convert number: %v\n", err)
				os.Exit(1)
			}
			cases = append(cases, n)
		}
		recs = append(recs, record{rr[i][0], rr[i][1], atof(rr[i][2]), atof(rr[i][3]), cases})
	}

	wg.Done()
	co <- data{rr[0], recs}
}

func (d data) filter(country string) (data, bool) {
	var rs []record
	for i := 0; i < len(d.records); i++ {
		if strings.EqualFold(d.records[i].country, country) {
			rs = append(rs, d.records[i])
		}
	}
	return data{d.header, rs}, (len(rs) > 0)
}

func (d data) sum(col int) int {
	if col < 0 {
		col = len(d.records[0].cases) + col
	}
	s := 0
	for _, r := range d.records {
		s += r.cases[col]
	}
	return s
}

func (d data) printCases(caseType string, colorFunc func(a ...interface{}) string) {
	t := d.sum(-1)
	n := t - d.sum(-2)
	fmt.Fprintf(w, "%v: \t%v \tNew: %v\n", caseType, colorFunc(p.Sprint(t)), colorFunc(p.Sprint(n)))
}

func (d data) sort() {
	sort.Slice(d.records, func(i, j int) bool {
		return d.records[i].cases[len(d.records[0].cases)-1] > d.records[j].cases[len(d.records[0].cases)-1]
	})
}

func atof(s string) float32 {
	n, err := strconv.ParseFloat(s, 32)
	if err != nil {
		fmt.Printf("Cannot convert number: %v\n", err)
		os.Exit(1)
	}
	return float32(n)
}
