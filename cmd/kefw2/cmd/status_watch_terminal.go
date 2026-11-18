// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
)

const watchArtworkMarkerPrefix = "\x00kefw2-art:"

type watchArtworkMarker uint64

func (m watchArtworkMarker) String() string {
	return fmt.Sprintf("%s%d\x00", watchArtworkMarkerPrefix, m)
}

// Bubble Tea flushes raw commands before its cell renderer. Hold image payloads
// until a requested full redraw has painted the cells beneath the image.
type watchArtworkWriter struct {
	output  io.Writer
	cancel  context.CancelFunc
	mu      sync.Mutex
	err     error
	serial  watchArtworkMarker
	payload string
	pending bool
	armed   bool
	active  bool
}

func (w *watchArtworkWriter) queue(payload string) watchArtworkMarker {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.serial++
	w.payload, w.pending, w.armed, w.active = payload, true, false, false
	return w.serial
}

func (w *watchArtworkWriter) invalidate() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.serial++
	w.payload, w.pending, w.armed, w.active = "", false, false, false
}

func (w *watchArtworkWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *watchArtworkWriter) write(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	n, err := w.output.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil && w.err == nil {
		w.err = err
		if w.cancel != nil {
			w.cancel()
		}
	}
	return err
}

func (w *watchArtworkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	originalLen := len(p)
	markerSeen := false
	for {
		start := bytes.Index(p, []byte(watchArtworkMarkerPrefix))
		if start < 0 {
			break
		}
		if err := w.write(p[:start]); err != nil {
			return 0, err
		}
		p = p[start+len(watchArtworkMarkerPrefix):]
		end := bytes.IndexByte(p, 0)
		if end < 0 {
			err := fmt.Errorf("incomplete terminal artwork marker")
			w.err = err
			if w.cancel != nil {
				w.cancel()
			}
			return 0, err
		}
		serial, err := strconv.ParseUint(string(p[:end]), 10, 64)
		if err != nil {
			err = fmt.Errorf("invalid terminal artwork marker: %w", err)
			w.err = err
			if w.cancel != nil {
				w.cancel()
			}
			return 0, err
		}
		if watchArtworkMarker(serial) == w.serial && w.pending {
			w.armed = true
		}
		markerSeen = true
		p = p[end+1:]
	}
	if err := w.write(p); err != nil {
		return 0, err
	}
	if !markerSeen && bytes.Contains(p, []byte("\x1b[2J")) && (w.armed || w.active) {
		if err := w.write([]byte(w.payload)); err != nil {
			return 0, err
		}
		w.armed, w.pending, w.active = false, false, true
	}
	return originalLen, nil
}
