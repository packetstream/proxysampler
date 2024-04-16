# ProxySampler. CLI tool for testing & measuring the health of proxy tunnels.

ProxySampler is a tool for testing proxies and measuring their performance & health.

## How can I use this?
Use this tool to check the performance and health of proxy tunnels. Test massive lists or one-off proxies. 

PacketStream customers can use this tool as part of their automation tooling to get metrics on which residential proxy sessions/geolocations are performing more reliably and prioritize those sessions. 

We use this tool internally at PacketStream as part of our infrastructure monitoring toolset to keep an eye on the the health of our proxy tunnel and trigger alerts if something doesn't seem right.

## Installation
#### Go

```bash
go get -u github.com/packetstream/proxysampler
```

## Usage and options
You provide the proxy server, port, and auth information. ProxySampler tests the proxy tunnel(s) and returns information about the response codes and response times.

ProxySampler is straightforward to use and has a few cli arguments for configuration.

```
Usage: 
  proxysampler [OPTIONS]

Application Options:
  --output {json|yaml|plaintext}    Default is plaintext.
  --include-response-body           Include response bodies in JSON/YAML output. Disabled by default.
  --file {/path/to/file.txt}        Relative/absolute path to a file containing a list of proxies.
  --proxy {proxy info}              Test a single proxy tunnel. Example: https://proxyuser:proxypass@packetstream.io:31111
  --endpoint {https://example.com}  The endpoint that you want to use for testing.
  --max-threads {10}                Number of concurrent threads to use for testing proxy. Default is 10.
  --delay {50}                      Delay in ms between each request. Default is 50.

Help Options
  --help, -h                        Show this screen.
```

### Usage examples

###### Testing a single PacketStream residential proxy with a US exit IP. JSON output.
```bash
proxysampler --output json --proxy https://proxyuser:proxypass_country-US@proxy.packetstream.io:31111 --endpoint https://example.com
```

```json
{
  "success": 1,
  "fail": 0,
  "average_ttfb": 221,
  "results": [
    {
      "proxy": "https://proxyuser:proxypass_country-US@proxy.packetstream.io:31111",
      "endpoint": "https://example.com",
      "status_code": 200,
      "latency": {
        "ttfb": 221,
        "connect": 34,
        "tls_handshake": 23
      },
      "error": null
    }
  ]
}
```

###### Testing a list of PacketStream residential proxies from a file. JSON output. Include response bodies.
```bash
proxysampler --output json --file ./proxies.txt --endpoint https://ifconfig.co/json --include-response-body
```

```json
{
  "success": 3,
  "fail": 0,
  "average_ttfb": 693,
  "results": [
    {
      "proxy": "https://proxyuser:proxypass_country-US@proxy.packetstream.io:31111",
      "endpoint": "https://ifconfig.co/json",
      "status_code": 200,
			"response_body": "{\"ip\":\"47.149.139.253\",\"ip_decimal\":798329853,\"country\":\"United States\",\"country_eu\":false,\"country_iso\":\"US\",\"city\":\"Torrance\",\"latitude\":33.846,\"longitude\":-118.3456,\"asn\":\"AS5650\",\"asn_org\":\"Frontier Communications of America, Inc.\"}",
      "latency": {
        "ttfb": 552,
        "connect": 29,
        "tls_handshake": 141
      },
      "error": null
    },
    {
      "proxy": "https://proxyuser:proxypass_country-US@proxy.packetstream.io:31111",
      "endpoint": "https://ifconfig.co/json",
      "status_code": 200,
      "response_body": "{\"ip\":\"63.237.69.254\",\"ip_decimal\":1072514558,\"country\":\"United States\",\"country_eu\":false,\"country_iso\":\"US\",\"city\":\"Walkersville\",\"hostname\":\"ssl.clarkconstruction.com\",\"latitude\":39.4787,\"longitude\":-77.3484,\"asn\":\"AS16431\",\"asn_org\":\"The Clark Construction Group, Inc.\"}",
      "latency": {
        "ttfb": 534,
        "connect": 28,
        "tls_handshake": 114
      },
      "error": null
    },
    {
      "proxy": "https://proxyuser:proxypass_country-US@proxy.packetstream.io:31111",
      "endpoint": "https://ifconfig.co/json",
      "status_code": 200,
      "response_body": "{\"ip\":\"24.167.234.46\",\"ip_decimal\":413657646,\"country\":\"United States\",\"country_eu\":false,\"country_iso\":\"US\",\"city\":\"Milwaukee\",\"hostname\":\"cpe-24-167-234-46.wi.res.rr.com\",\"latitude\":43.1166,\"longitude\":-87.9904,\"asn\":\"AS10796\",\"asn_org\":\"Charter Communications Inc\"}",
      "latency": {
        "ttfb": 993,
        "connect": 27,
        "tls_handshake": 272
      },
      "error": null
    }
  ]
}
```

## Contributing
We'd love to accept your improvements! Feel free to fork & submit a pull-request if you'd like to make changes.

## About PacketStream
PacketStream is a residential proxy network that enables businesses to access the web through a pool of residential IPs. With PacketStream, you can gather data, scrape websites, and perform other web-related tasks with requests originating from real residential ISPs all over the world.

For more information, visit [PacketStream.io](https://packetstream.io/).
