// Package tailscalesd provides Prometheus Service Discovery for Tailscale.
package tailscalesd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
)

// TargetDescriptor as Prometheus expects it. For more details, see
// https://prometheus.io/docs/prometheus/latest/http_sd/.
type TargetDescriptor struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

const (
	// LabelMetaAPI is the host which provided the details about this device.
	// Will be "localhost" for the local API.
	LabelMetaAPI = "__meta_tailscale_api"

	// LabelMetaDeviceAuthorized is whether the target is currently authorized on the Tailnet.
	// Will always be true when using the local API.
	LabelMetaDeviceAuthorized = "__meta_tailscale_device_authorized"

	// LabelMetaDeviceClientVersion is the Tailscale client version in use on
	// target. Not reported when using the local API.
	LabelMetaDeviceClientVersion = "__meta_tailscale_device_client_version"

	// LabelMetaDeviceHostname is the short hostname of the device.
	LabelMetaDeviceHostname = "__meta_tailscale_device_hostname"

	// LabelMetaDeviceID is the target's unique ID within Tailscale, as reported
	// by the API. The public API reports this as a large integer. The local API
	// reports a base64 string.
	// string.
	LabelMetaDeviceID = "__meta_tailscale_device_id"

	// LabelMetaDeviceName is the name of the device as reported by the API. Not
	// reported when using the local API.
	LabelMetaDeviceName = "__meta_tailscale_device_name"

	// LabelMetaDeviceOS is the OS of the target.
	LabelMetaDeviceOS = "__meta_tailscale_device_os"

	// LabelMetaDeviceTag is a Tailscale ACL tag applied to the target.
	LabelMetaDeviceTag = "__meta_tailscale_device_tag"

	// LabelMetaTailnet is the name of the Tailnet from which this target
	// information was retrieved. Not reported when using the local API.
	LabelMetaTailnet = "__meta_tailscale_tailnet"
)

// filterEmpty removes entries in a map which have either an empty key or empty
// value.
func filterEmpty(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	filtered := make(map[string]string)
	for k, v := range in {
		if k == "" || v == "" {
			continue
		}
		filtered[k] = v
	}
	return filtered
}

type filter func(TargetDescriptor) TargetDescriptor

func filterIPv6Addresses(td TargetDescriptor) TargetDescriptor {
	var targets []string
	for _, target := range td.Targets {
		ip := net.ParseIP(target)
		if ip == nil {
			// target is not a valid IP address of any version.
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			targets = append(targets, ipv4.String())
		}
	}
	return TargetDescriptor{
		Targets: targets,
		Labels:  td.Labels,
	}
}

func filterEmptyLabels(td TargetDescriptor) TargetDescriptor {
	return TargetDescriptor{
		Targets: td.Targets,
		Labels:  filterEmpty(td.Labels),
	}
}

// translate Devices to Prometheus TargetDescriptor, filtering empty labels.
func translate(devices []Device, filters ...filter) (found []TargetDescriptor) {
	for _, d := range devices {
		target := TargetDescriptor{
			Targets: d.Addresses,
			// All labels added here, except for tags.
			Labels: map[string]string{
				LabelMetaAPI:                 d.API,
				LabelMetaDeviceAuthorized:    fmt.Sprint(d.Authorized),
				LabelMetaDeviceClientVersion: d.ClientVersion,
				LabelMetaDeviceHostname:      d.Hostname,
				LabelMetaDeviceID:            d.ID,
				LabelMetaDeviceName:          d.Name,
				LabelMetaDeviceOS:            d.OS,
				LabelMetaTailnet:             d.Tailnet,
			},
		}
		for _, filter := range filters {
			target = filter(target)
		}
		if l := len(d.Tags); l == 0 {
			found = append(found, target)
			continue
		}
		for _, t := range d.Tags {
			lt := target
			lt.Labels = make(map[string]string)
			for k, v := range target.Labels {
				lt.Labels[k] = v
			}
			lt.Labels[LabelMetaDeviceTag] = t
			found = append(found, lt)
		}
	}
	return
}

type discoveryHandler struct {
	ts      Client
	filters []filter
}

func (h *discoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	devices, err := h.ts.Devices(r.Context())
	if err != nil {
		if err != ErrStaleResults {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to discover Tailscale devices: %v", err)
			fmt.Fprintf(w, "Failed to discover Tailscale devices: %v", err)
			return
		}
		log.Print("Serving potentially stale results")
	}
	targets := translate(devices, h.filters...)

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(targets); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed encoding targets to JSON: %v", err)
		fmt.Fprintf(w, "Failed encoding targets to JSON: %v", err)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	if _, err := io.Copy(w, &buf); err != nil {
		// The transaction with the client is already started, so there's nothing
		// graceful to do here. Log any errors for troubleshooting later.
		log.Printf("Failed sending JSON payload to the client: %v", err)
	}
}

// Export the Tailscale client for Service Discovery.
func Export(ts Client) http.Handler {
	return &discoveryHandler{
		ts: ts,
		// TODO(cfunkhouser): Make these filters configurable.
		filters: []filter{filterEmptyLabels, filterIPv6Addresses},
	}
}
