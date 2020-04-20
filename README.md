# covid
Shows number of COVID-19 cases.

## Install
`GO111MODULE=on go get github.com/m4ns0ur/covid`
Note that `$GOPATH/bin` should be in the path.

## Usage
```
$ covid -h
Shows number of COVID-19 cases.

Usage:
  covid [flags]

Flags:
  -e, --cache            enable request caching (default true)
  -c, --country string   country to show number of cases for
      --graph            plot graph, only if country is selected
  -h, --help             help for covid
  -s, --save             save/overwrite data in file (default true)
  -t, --top-confirmed    Top 10 countries by most confirmed cases
      --top-dead         Top 10 countries by most dead cases
      --top-recovered    Top 10 countries by most recovered cases
  -v, --verbose          more verbose operation information
```

## Screenshot
![screenshot-1](/.res/screenshot-1.png)

## Dataset
It's using data provided by [Johns Hopkins CSSE](https://github.com/CSSEGISandData/COVID-19/tree/master/csse_covid_19_data/csse_covid_19_time_series).

## License
MIT - see [LICENSE][license]

[license]: https://github.com/m4ns0ur/covid/blob/master/LICENSE
