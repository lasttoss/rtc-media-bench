// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js

package main

import (
	"fmt"
	"net"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// api is the WebRTC API every peer connection goes through. It is built once in main() after the
// flags are parsed.
var api *webrtc.API

// setupAPI builds the API and stores it for the signalling handler.
func setupAPI() error {
	a, err := newAPI()
	if err != nil {
		return err
	}
	api = a
	return nil
}

// newAPI builds the WebRTC API with the ICE configuration from the command line.
//
// Local addition to upstream sfu-ws (see patches/0001-ice-config.patch). Nothing here touches
// media: it decides which addresses and ports ICE offers, which is the part a cluster, a
// firewall and a NAT each have an opinion about.
func newAPI() (*webrtc.API, error) {
	var se webrtc.SettingEngine

	// Offer IPv4 unless IPv6 is asked for. A host with a link-local IPv6 address - a docker bridge,
	// a veth pair - otherwise advertises an address that no client can use, and every client pays
	// for it in connectivity checks before falling back to the one that works.

	// Note on what is NOT configured here: restricting ICE to IPv4. SetNetworkTypes(UDP4) looks
	// like it should do that, and with a UDPMux it does not - the mux socket covers every interface
	// the machine has, so the peer still advertises IPv6 link-local candidates. Measured, not
	// assumed: with -advertise-ip set to an unroutable address, two participants on one host still
	// connected in 18 ms over an IPv6 link-local candidate nobody asked for. A cluster without IPv6
	// has no such escape route, which is exactly why the same configuration fails there - see
	// docs/runbook.md.
	if *advertiseIP != "" {
		// Advertise a different IP than the one the socket is bound to. This is what a NAT or a
		// load balancer forces you to do - and doing it wrong is the single most common reason a
		// call connects, then dies: the candidate looks valid and is never reachable.
		se.SetNAT1To1IPs([]string{*advertiseIP}, webrtc.ICECandidateTypeHost)
	}

	switch {
	case *udpMuxPort != 0:
		// One socket, many candidates: the only shape that survives a Service or a firewall with
		// a finite port range.
		mux, err := ice.NewMultiUDPMuxFromPort(*udpMuxPort)
		if err != nil {
			return nil, fmt.Errorf("udp mux on %d: %w", *udpMuxPort, err)
		}
		se.SetICEUDPMux(mux)
	case *udpPortMin != 0:
		se.SetEphemeralUDPPortRange(uint16(*udpPortMin), uint16(*udpPortMax))
	}

	if *iceTCPPort != 0 {
		ln, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: *iceTCPPort})
		if err != nil {
			return nil, fmt.Errorf("ice tcp listener on %d: %w", *iceTCPPort, err)
		}
		se.SetICETCPMux(ice.NewTCPMuxDefault(ice.TCPMuxParams{Listener: ln}))
	}

	return webrtc.NewAPI(webrtc.WithSettingEngine(se)), nil
}
