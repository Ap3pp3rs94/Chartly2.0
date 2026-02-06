#!/usr/bin/env pwsh
$base = "C:\Chartly2.0\profiles\government"
New-Item -ItemType Directory -Force -Path $base | Out-Null

$remove = @(
  "data-gov.yaml",
  "data-gov-catalog-climate.yaml",
  "data-gov-catalog-energy.yaml",
  "data-gov-catalog-health.yaml",
  "data-gov-catalog-latest.yaml",
  "data-gov-catalog-transportation.yaml",
  "data-cdc-chronic.yaml",
  "data-cdc-chronic-disease.yaml",
  "data-la-crime.yaml",
  "data-la-crime-2020.yaml",
  "data-mc-warehouse-sales.yaml",
  "data-moco-retail.yaml",
  "data-ny-powerball.yaml",
  "data-wa-ev-population.yaml"
)
foreach ($f in $remove) {
  $p = Join-Path $base $f
  if (Test-Path $p) { Remove-Item -LiteralPath $p -Force }
}

$coins = @(
  @{ id="bitcoin"; sym="btc"; name="Bitcoin" },
  @{ id="ethereum"; sym="eth"; name="Ethereum" },
  @{ id="solana"; sym="sol"; name="Solana" },
  @{ id="cardano"; sym="ada"; name="Cardano" },
  @{ id="ripple"; sym="xrp"; name="XRP" },
  @{ id="dogecoin"; sym="doge"; name="Dogecoin" },
  @{ id="litecoin"; sym="ltc"; name="Litecoin" },
  @{ id="polkadot"; sym="dot"; name="Polkadot" },
  @{ id="chainlink"; sym="link"; name="Chainlink" },
  @{ id="avalanche-2"; sym="avax"; name="Avalanche" }
)

$horizons = @(
  @{ days="1"; label="1h" },
  @{ days="1"; label="24h" },
  @{ days="7"; label="7d" }
)

foreach ($c in $coins) {
  foreach ($h in $horizons) {
    $id = "coingecko-$($c.sym)-usd-$($h.label)"
    $path = Join-Path $base ("$id.yaml")
    $yaml = @"
id: $id
name: CoinGecko $($c.name) USD ($($h.label))
version: 1.0.0
description: $($c.name) price in USD, horizon $($h.label)
schedule:
  enabled: true
  interval: 5s
  jitter: 1s
limits:
  max_records: 5000
  max_pages: 50
  max_bytes: 1048576
source:
  type: http_rest
  url: https://api.coingecko.com/api/v3/coins/$($c.id)/market_chart?vs_currency=usd&days=$($h.days)&interval=hourly
  auth: none
mapping:
  [0]: dims.time.timestamp
  [1]: measures.crypto.price_usd
"@
    Set-Content -LiteralPath $path -Encoding utf8 -Value $yaml
  }
}

$open = @'
id: open-meteo-nyc-hourly
name: Open-Meteo NYC Hourly Weather
version: 1.0.0
description: Hourly temperature, humidity, and wind for NYC (UTC)
schedule:
  enabled: true
  interval: 5s
  jitter: 1s
limits:
  max_records: 5000
  max_pages: 50
  max_bytes: 1048576
source:
  type: http_rest
  url: https://api.open-meteo.com/v1/forecast?latitude=40.7128&longitude=-74.0060&hourly=temperature_2m,relative_humidity_2m,wind_speed_10m&wind_speed_unit=kmh&timezone=UTC
  auth: none
mapping:
  time: dims.time.timestamp
  temperature_2m: measures.weather.temp_c
  relative_humidity_2m: measures.weather.humidity_pct
  wind_speed_10m: measures.weather.wind_kph
'@
Set-Content -LiteralPath (Join-Path $base "open-meteo-nyc-hourly.yaml") -Encoding utf8 -Value $open

Get-ChildItem -LiteralPath $base -Filter "*.yaml" | Select-Object Name
