# TailscaleSD - Prometheus Service Discovery for Tailscale

Serves Prometheus HTTP Service Discovery for devices on a Tailscale Tailnet.

For details on HTTP Service Discovery, read the Prometheus docs:
https://prometheus.io/docs/prometheus/latest/http_sd/

## Usage

The `tailscalesd` server is very simple. It serves the SD payload at `/` on its
HTTP server. It respects four configuration parameters, each of which may be
specified as a flag or an environment variable.

- `-address` / `ADDRESS` is the host:port on which to serve TailscaleSD.
  Defaults to `0.0.0.0:9242`.
- `-poll` / `TAILSCALE_API_POLL_LIMIT` is the limit of how frequently the
  Tailscale API may be polled. Cached results are served between intervals.
  Defaults to 5 minutes. Also applies to local API.
- `-localapi` / `TAILSCALE_USE_LOCAL_API` instructs TailscaleSD to use the
  `tailscaled`-exported local API for discovery.
- `-tailnet` / `TAILNET` is the name of the tailnet to enumerate. Required
  when using the public API.
- `-token` / `TAILSCALE_API_TOKEN` is a Tailscale API token with appropriate
  permissions to access the Tailscale API and enumerate devices. Required when
  using the public API.

```console
$ TAILSCALE_API_TOKEN=SUPERSECRET tailscalesd --tailnet alice@gmail.com
2021-08-04T15:38:14Z Serving Tailscale service discovery on "0.0.0.0:9242"
```

### Public vs Local API

TailscaleSD is capable of discovering devices both from Tailscale's public API,
and from the local API served by `tailscaled` on the node on which TailscaleSD
is run. By using the public API, TailscaleSD will dicover _all_ devices in the
tailnet, regardless of whether the local node is able to reach them or not.
Devices found using the local API will be reachable from the local node,
according to your Tailscale ACLs.

See the label comments in [`tailscalesd.go`](./tailscalesd.go) for details about
which labels are supported for each API type. **Do not assume they will be the
same labels, or that values will match across the APIs!**

## Prometheus Configuration

Configure Prometheus by placing the `tailscalesd` URL in a `http_sd_configs`
block in a `scrape_config`. The following labels are potentially made available
for all Tailscale nodes discovered, however any label for which the Tailscale
API did not return a value will be omitted. For more details on each field and
the API in general, see:
https://github.com/tailscale/tailscale/blob/main/api.md#tailnet-devices-get

Possible target labels follow. See the label comments in [`tailscalesd.go`](./tailscalesd.go) for details.

- `__meta_tailscale_api`
- `__meta_tailscale_device_authorized`
- `__meta_tailscale_device_client_version`
- `__meta_tailscale_device_hostname`
- `__meta_tailscale_device_id`
- `__meta_tailscale_device_name`
- `__meta_tailscale_device_os`
- `__meta_tailscale_tailnet`

### Example: Pinging Tailscale Hosts

In the example below, Prometheus will discover Tailscale nodes and attempt to
ping them using a blackbox exporter.

```yaml
---
global:
  scrape_interval: 1m
scrape_configs:
- job_name: tailscale-prober
    metrics_path: /probe
    params:
      module: [icmp]
    http_sd_configs:
      - url: http://localhost:9242/
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - source_labels: [__meta_tailscale_device_hostname]
        target_label: tailscale_hostname
      - source_labels: [__meta_tailscale_device_name]
        target_label: tailscale_name
      - target_label: __address__
        replacement: your.blackbox.exporter:9115
```

### Example: Scraping Node Exporter from Tailscale Hosts

This example appends the node exporter port `9100` to the addresses returned
from the Tailscale API, and instructs Prometheus to collect those metrics. This
is likely to result in many "down" targets if your tailnet contains hosts
without the node exporter. It also doesn't play well with IPv6 addresses.

```yaml
---
global:
  scrape_interval: 1m
scrape_configs:
- job_name: tailscale-node-exporter
    http_sd_configs:
      - url: http://localhost:9242/
    relabel_configs:
      - source_labels: [__meta_tailscale_device_hostname]
        target_label: tailscale_hostname
      - source_labels: [__meta_tailscale_device_name]
        target_label: tailscale_name
      - source_labels: [__address__]
        regex: '(.*)'
        replacement: $1:9100
        target_label: __address__
```