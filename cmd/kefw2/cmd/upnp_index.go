/*
Copyright © 2023-2026 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// Package cmd provides CLI commands for kefw2.
// Track index functionality has been moved to the kefw2 library:
//   - kefw2.IndexedTrack, kefw2.TrackIndex
//   - kefw2.LoadTrackIndex(), kefw2.SaveTrackIndex()
//   - kefw2.BuildTrackIndex(), kefw2.SearchTracks()
//   - kefw2.TrackIndexPath(), kefw2.IsTrackIndexFresh()
//   - kefw2.FindContainerByPath(), kefw2.ListContainersAtPath()
//   - kefw2.IndexedTrackToContentItem(), kefw2.FormatTrackDuration()
package cmd

import (
	"time"
)

// CLI-specific constants for track indexing.
// TypeContainer and TypeAudio are defined in constants.go.
const (
	// defaultIndexMaxAge is the default maximum age for the track index.
	// If the index is older than this, it will be considered stale.
	defaultIndexMaxAge = 24 * time.Hour
)
