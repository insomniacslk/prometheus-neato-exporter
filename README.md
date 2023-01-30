# prometheus-neato-exporter

This is a Neato exporter for Prometheus. It requires a BotVac Neato vacuum
cleaner.

It exports the following metrics:
* `neato_battery`
* `neato_state`

## Run it
```
go build
./prometheus-neato-exporter -t your_token
```

To get the token, run `neato login` from https://github.com/insomniacslk/neato
(CLI under `cmd/neato`).

If you want to monitor a specific bot or a subset of your bots,
use `--bots`, for example `--bots 1,3` will monitors bots 1 and 3
