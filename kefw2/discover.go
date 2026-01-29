package kefw2

import (
	"context"
	"time"

	"github.com/brutella/dnssd"
)

// DiscoverSpeakers searches for KEF speakers on the local network using mDNS.
// The timeout parameter specifies how long to wait for discovery in seconds.
func DiscoverSpeakers(ctx context.Context, timeout time.Duration) ([]*KEFSpeaker, error) {
	ips, err := discoverIPs(ctx, timeout)
	if err != nil {
		return nil, err
	}

	// Use a map for deduplication by IP
	speakersByIP := make(map[string]*KEFSpeaker)
	for _, ip := range ips {
		if _, exists := speakersByIP[ip]; exists {
			continue // Already discovered
		}

		speaker, err := NewSpeaker(ip)
		if err != nil {
			continue // Skip speakers we can't connect to
		}
		speakersByIP[ip] = speaker
	}

	// Convert map to slice
	speakers := make([]*KEFSpeaker, 0, len(speakersByIP))
	for _, s := range speakersByIP {
		speakers = append(speakers, s)
	}

	return speakers, nil
}

// DiscoverSpeakersLegacy is the old API that takes timeout in seconds as an int.
// Deprecated: Use DiscoverSpeakers with context and time.Duration instead.
func DiscoverSpeakersLegacy(timeout int) ([]*KEFSpeaker, error) {
	return DiscoverSpeakers(context.Background(), time.Duration(timeout)*time.Second)
}

func discoverIPs(ctx context.Context, timeout time.Duration) ([]string, error) {
	// Create a context with timeout if not already set
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var ips []string
	discovered := make(chan string, 100)
	done := make(chan struct{})

	service := "_http._tcp.local."

	addFn := func(e dnssd.BrowseEntry) {
		if len(e.IPs) > 0 {
			select {
			case discovered <- e.IPs[0].String():
			default:
				// Channel full, skip
			}
		}
	}

	rmvFn := func(e dnssd.BrowseEntry) {} // Not needed for discovery

	// Start discovery in background
	go func() {
		defer close(done)
		_ = dnssd.LookupType(ctx, service, addFn, rmvFn)
	}()

	// Collect discovered IPs until context is done
	for {
		select {
		case ip := <-discovered:
			ips = append(ips, ip)
		case <-ctx.Done():
			// Drain any remaining IPs
			for {
				select {
				case ip := <-discovered:
					ips = append(ips, ip)
				default:
					return ips, nil
				}
			}
		case <-done:
			// Discovery finished early
			for {
				select {
				case ip := <-discovered:
					ips = append(ips, ip)
				default:
					return ips, nil
				}
			}
		}
	}
}
