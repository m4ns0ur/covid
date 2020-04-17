# covid
Show number of COVID-19 cases.

## Install
`go get github.com/m4ns0ur/covid`

## Use
```
$ covid -h
Show number of COVID-19 cases.

Usage:
  covid [flags]

Flags:
  -e, --cache            enable request caching (default true)
  -c, --country string   country to show number of cases for
  -h, --help             help for covid
  -s, --save             save/overwrite data in file (default true)
  -v, --verbose          more verbose operation information
```
## Dataset
It's using data provided by [Johns Hopkins CSSE](https://github.com/CSSEGISandData/COVID-19/tree/master/csse_covid_19_data/csse_covid_19_time_series).

## License
MIT - see [LICENSE][license]

[license]: https://github.com/m4ns0ur/covid/blob/master/LICENSE
