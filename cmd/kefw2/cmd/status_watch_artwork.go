// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/BourgeoisBear/rasterm"
	"github.com/hilli/icat"
	ascii "github.com/qeesung/image2ascii/convert"
)

const watchImageID = 637245

const clearWatchImage = "\x1b_Ga=d,d=I,i=637245,q=2\x1b\\"

type watchArtworkBackend int

const (
	watchArtworkASCII watchArtworkBackend = iota
	watchArtworkKitty
	watchArtworkIterm
)

func detectWatchArtworkBackend(forceASCII bool) watchArtworkBackend {
	if forceASCII || os.Getenv("TMUX") != "" || rasterm.IsTmuxScreen() {
		return watchArtworkASCII
	}
	if rasterm.IsKittyCapable() {
		return watchArtworkKitty
	}
	if os.Getenv("TERM_PROGRAM") == "iTerm.app" || rasterm.IsItermCapable() {
		return watchArtworkIterm
	}
	return watchArtworkASCII
}

type watchArtwork struct {
	image  image.Image
	digest [sha256.Size]byte
}

func fetchWatchArtwork(ctx context.Context, url string) (watchArtwork, error) {
	var art watchArtwork
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return art, fmt.Errorf("requesting artwork: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return art, fmt.Errorf("fetching artwork: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return art, fmt.Errorf("fetching artwork: HTTP %d", resp.StatusCode)
	}
	const maxBytes = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return art, fmt.Errorf("reading artwork: %w", err)
	}
	if len(data) > maxBytes {
		return art, fmt.Errorf("artwork exceeds 8 MiB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return art, fmt.Errorf("decoding artwork dimensions: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16_000_000/config.Height {
		return art, fmt.Errorf("artwork exceeds 16 megapixels")
	}
	img, err := icat.DecodeImage(data)
	if err != nil {
		return art, fmt.Errorf("decoding artwork: %w", err)
	}
	art.image, art.digest = *img, sha256.Sum256(data)
	return art, nil
}

// Keep these lines unchanged between text updates. Bubble Tea then skips the
// entire reserved region, preserving the native image without retransmission.
func renderWatchArtwork(art watchArtwork, width, height int, backend watchArtworkBackend) ([]string, error) {
	if art.image == nil || width < 1 || height < 1 {
		return nil, nil
	}
	if width > 65535 || height > 65535 {
		return nil, fmt.Errorf("artwork terminal dimensions exceed 65535 cells")
	}
	if backend == watchArtworkIterm {
		return renderWatchItermArtwork(art, width, height)
	}
	// Terminal cells are approximately twice as tall as they are wide.
	// Native placement specifies only the limiting axis so the terminal keeps
	// the exact image aspect ratio, even with a different font cell size.
	bounds := art.image.Bounds()
	cellAspect := 2 * float64(bounds.Dx()) / float64(bounds.Dy())
	opts := rasterm.KittyImgOpts{ImageId: watchImageID}
	if float64(width)/float64(height) > cellAspect {
		width = max(1, int(math.Ceil(float64(height)*cellAspect)))
		opts.DstRows = uint32(height)
	} else {
		height = max(1, int(math.Ceil(float64(width)/cellAspect)))
		opts.DstCols = uint32(width)
	}
	if backend == watchArtworkASCII {
		options := ascii.DefaultOptions
		options.FixedWidth, options.FixedHeight = width, height
		options.FitScreen, options.Colored = false, false
		text := ascii.NewImageConverter().Image2ASCIIString(art.image, &options)
		return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), nil
	}
	var payload bytes.Buffer
	if err := rasterm.KittyWriteImage(&payload, art.image, opts); err != nil {
		return nil, fmt.Errorf("encoding terminal artwork: %w", err)
	}
	encoded := strings.Replace(payload.String(), "\x1b_Ga=T,", "\x1b_Ga=T,q=2,", 1)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = strings.Repeat(" ", width)
	}
	lines[0] = "\x1b7" + clearWatchImage + encoded + "\x1b8" + lines[0]
	return lines, nil
}

func renderWatchItermArtwork(art watchArtwork, width, height int) ([]string, error) {
	var payload bytes.Buffer
	if err := rasterm.ItermWriteImageWithOptions(&payload, art.image, rasterm.ItermImgOpts{
		Width: fmt.Sprint(width), Height: fmt.Sprint(height), DisplayInline: true,
	}); err != nil {
		return nil, fmt.Errorf("encoding iTerm artwork: %w", err)
	}
	// Paint the image after the reserved rows: unlike Kitty placements, iTerm
	// images are erased by text written over their cells.
	lines := make([]string, height)
	var erase strings.Builder
	erase.WriteString("\x1b7\r")
	if height > 1 {
		fmt.Fprintf(&erase, "\x1b[%dA", height-1)
	}
	for i := range lines {
		lines[i] = strings.Repeat(" ", width)
		fmt.Fprintf(&erase, "\x1b[%dX", width)
		if i+1 < height {
			erase.WriteString("\x1b[1B")
		}
	}
	if height > 1 {
		fmt.Fprintf(&erase, "\x1b[%dA", height-1)
	}
	lines[height-1] += erase.String() + payload.String() + "\x1b8"
	return lines, nil
}
