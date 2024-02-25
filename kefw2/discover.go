package kefw2

import (
	"context"
	"time"

	"github.com/brutella/dnssd"
)

func DiscoverSpeakers(timeout int) ([]KEFSpeaker, error) {
	discoveredSpeakers := []KEFSpeaker{}
	ips, err := discoverIPs(timeout)
	if err != nil {
		return discoveredSpeakers, err
	}
	for _, ip := range ips {
		speaker, err := NewSpeaker(ip)
		if err != nil {
			continue
		}
		discoveredSpeakers = append(discoveredSpeakers, speaker)
	}

	// Service Discovery may have the same speakers multiple times. Lets filter it down to single instance
	found := map[string]KEFSpeaker{}
	for _, s := range discoveredSpeakers {
		found[s.IPAddress] = s
	}
	discoveredSpeakers = []KEFSpeaker{}
	for _, s := range found {
		discoveredSpeakers = append(discoveredSpeakers, s)
	}

	return discoveredSpeakers, nil
}

func discoverIPs(timeout int) ([]string, error) {
	ips := []string{}
	waitForDiscoveryTimeout := time.Duration(timeout) * time.Second
	discoveryTimeout := time.Duration(timeout) * time.Second
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(discoveryTimeout))

	defer cancel()
	service := "_http._tcp.local."

	addFn := func(e dnssd.BrowseEntry) {
		ips = append(ips, e.IPs[0].String())
	}

	rmvFn := func(e dnssd.BrowseEntry) {} // Empty, don't need it

	go func(ctx context.Context) {
		defer cancel()
		if err := dnssd.LookupType(ctx, service, addFn, rmvFn); err != nil {
			return
		}
	}(ctx)

	time.Sleep(waitForDiscoveryTimeout)

	return ips, nil
}
