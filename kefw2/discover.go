package kefw2

import (
	"context"
	"time"

	"github.com/brutella/dnssd"
)

func DiscoverSpeakers() ([]KEFSpeaker, error) {
	discoveredSpeakers := []KEFSpeaker{}
	ips, err := discoverIPs()
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
	return discoveredSpeakers, nil
}

func discoverIPs() ([]string, error) {
	ips := []string{}
	waitForDiscoveryTimeout := 1 * time.Second
	discoveryTimeout := 1 * time.Second
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
