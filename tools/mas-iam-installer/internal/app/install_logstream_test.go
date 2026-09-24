package app

import (
	"strings"
	"testing"
)

func TestResumeWriterStripsTimestamps(t *testing.T) {
	var out strings.Builder
	w := &resumeWriter{out: &out}
	_, _ = w.Write([]byte("2026-09-24T01:08:48.354236449Z [wait] currentCSV=v0.0.15\n2026-09-24T01:09:18.356193021Z [install] applying operator Subscription\n"))

	want := "[wait] currentCSV=v0.0.15\n[install] applying operator Subscription\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
	if got := w.SinceTime(); got != "2026-09-24T01:09:18.356193021Z" {
		t.Fatalf("SinceTime = %q", got)
	}
}

// After a reconnect with --since-time (inclusive), the boundary line and
// anything older must not print again.
func TestResumeWriterSkipsReplayedLinesAfterReconnect(t *testing.T) {
	var out strings.Builder
	w := &resumeWriter{out: &out}
	_, _ = w.Write([]byte("2026-09-24T01:08:48.000000001Z one\n2026-09-24T01:08:49.000000001Z two\n2026-09-24T01:08:50.0000"))
	w.Resume() // connection dropped mid-line

	_, _ = w.Write([]byte("2026-09-24T01:08:49.000000001Z two\n2026-09-24T01:08:50.000000001Z three\n"))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	if out.String() != "one\ntwo\nthree\n" {
		t.Fatalf("output = %q, want one/two/three exactly once", out.String())
	}
}

func TestResumeWriterHandlesSplitWritesAndUntimestampedLines(t *testing.T) {
	var out strings.Builder
	w := &resumeWriter{out: &out}
	_, _ = w.Write([]byte("2026-09-24T01:08:48.000000001Z hel"))
	_, _ = w.Write([]byte("lo\nno timestamp here\n2026-09-24T01:08:49.000000001Z last without newline"))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "hello\nno timestamp here\nlast without newline\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// Regression, seen live: the runtime gave "line-3" and "line-4" the identical
// timestamp. Neither may be dropped in the first stream, and after a resume
// only the lines already shown at that timestamp are skipped.
func TestResumeWriterKeepsLinesSharingATimestamp(t *testing.T) {
	var out strings.Builder
	w := &resumeWriter{out: &out}
	_, _ = w.Write([]byte("2026-09-24T01:52:47.199120069Z line-3\n"))
	w.Resume() // dropped after line-3
	_, _ = w.Write([]byte("2026-09-24T01:52:47.199120069Z line-3\n2026-09-24T01:52:47.199120069Z line-4\n"))
	if out.String() != "line-3\nline-4\n" {
		t.Fatalf("after resume output = %q, want line-3 then line-4 once each", out.String())
	}

	out.Reset()
	w = &resumeWriter{out: &out}
	_, _ = w.Write([]byte("2026-09-24T01:52:47.199120069Z a\n2026-09-24T01:52:47.199120069Z b\n"))
	if out.String() != "a\nb\n" {
		t.Fatalf("same-stream output = %q, want both lines", out.String())
	}
}
