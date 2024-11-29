package taglib_test

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r3labs/diff/v3"
	"go.senan.xyz/taglib"
)

var (
	//go:embed testdata/eg.flac
	egFLAC []byte
	//go:embed testdata/eg.mp3
	egMP3 []byte
	//go:embed testdata/eg.m4a
	egM4a []byte
)

func TestInvalid(t *testing.T) {
	path := tmpf(t, []byte("not a file"), "eg.flac")
	_, err := taglib.ReadTags(path)
	eq(t, err, taglib.ErrInvalidFile)
}

func TestClear(t *testing.T) {
	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			// set some tags first
			err := taglib.WriteTags(path, map[string][]string{
				"ARTIST":     {"Example A"},
				"ALUMARTIST": {"Example"},
			})
			nilErr(t, err)

			// then clear
			err = taglib.WriteTags(path, nil)
			nilErr(t, err)

			got, err := taglib.ReadTags(path)
			nilErr(t, err)

			difft(t, nil, got)
		})
	}
}

func TestReadWrite(t *testing.T) {
	paths := testPaths(t)
	testTags := []map[string][]string{
		{
			"ONE":  {"one", "two", "three", "four"},
			"FIVE": {"six", "seven"},
			"NINE": {"nine"},
		},
		{
			"ARTIST":     {"Example A"},
			"ALUMARTIST": {"Example"},
		},
		{
			"ARTIST":      {"Example A", "Example B"},
			"ALUMARTIST":  {"Example"},
			"TRACK":       {"1"},
			"TRACKNUMBER": {"1"},
		},
		{
			"ARTIST":     {"Example A", "Example B"},
			"ALUMARTIST": {"Example"},
		},
	}

	for _, path := range paths {
		for i, tags := range testTags {
			t.Run(fmt.Sprintf("%s_tags_%d", filepath.Base(path), i), func(t *testing.T) {
				err := taglib.WriteTags(path, tags)
				nilErr(t, err)

				got, err := taglib.ReadTags(path)
				nilErr(t, err)

				difft(t, tags, got)
			})
		}
	}
}

func TestConcurrent(t *testing.T) {
	paths := testPaths(t)

	c := 250
	pathErrors := make([]error, c)

	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := taglib.ReadTags(paths[i%len(paths)]); err != nil {
				pathErrors[i] = fmt.Errorf("iter %d: %w", i, err)
			}
		}()
	}
	wg.Wait()

	err := errors.Join(pathErrors...)
	nilErr(t, err)
}

func TestAudioProperties(t *testing.T) {
	path := tmpf(t, egFLAC, "eg.flac")

	properties, err := taglib.ReadProperties(path)
	nilErr(t, err)

	eq(t, 1*time.Second, properties.Length)
	eq(t, 1460, properties.Bitrate)
	eq(t, 48_000, properties.SampleRate)
	eq(t, 2, properties.Channels)
}

func TestMultiOpen(t *testing.T) {
	{
		path := tmpf(t, egFLAC, "eg.flac")
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
	}
	{
		path := tmpf(t, egFLAC, "eg.flac")
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
	}
}

func TestMemNew(t *testing.T) {
	t.Skip("heavy")

	checkMem(t)

	for range 10_000 {
		path := tmpf(t, egFLAC, "eg.flac")
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
		err = os.Remove(path) // don't blow up incase we're using tmpfs
		nilErr(t, err)
	}
}

func TestMemSameFile(t *testing.T) {
	t.Skip("heavy")

	checkMem(t)

	path := tmpf(t, egFLAC, "eg.flac")
	for range 10_000 {
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	t.Logf("alloc = %v MiB", memStats.Alloc/1024/1024)
}

func BenchmarkWrite(b *testing.B) {
	path := tmpf(b, egFLAC, "eg.flac")
	b.ResetTimer()

	t := map[string][]string{
		"ARTIST": {"Artist"},
	}

	for range b.N {
		err := taglib.WriteTags(path, t)
		nilErr(b, err)
	}
}

func BenchmarkRead(b *testing.B) {
	path := tmpf(b, egFLAC, "eg.flac")
	b.ResetTimer()

	for range b.N {
		_, err := taglib.ReadTags(path)
		nilErr(b, err)
	}
}

func checkMem(t testing.TB) {
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})

	go func() {
		ticker := time.Tick(100 * time.Millisecond)

		for {
			select {
			case _, ok := <-stop:
				if !ok {
					return
				}

			case <-ticker:
				var memStats runtime.MemStats
				runtime.ReadMemStats(&memStats)
				t.Logf("alloc = %v MiB", memStats.Alloc/1024/1024)
			}
		}
	}()
}

func testPaths(t testing.TB) []string {
	return []string{
		tmpf(t, egMP3, "eg.mp3"),
		tmpf(t, egM4a, "eg.m4a"),
		tmpf(t, egFLAC, "eg.flac"),
	}
}

func tmpf(t testing.TB, b []byte, name string) string {
	p := filepath.Join(t.TempDir(), name)
	err := os.WriteFile(p, b, os.ModePerm)
	nilErr(t, err)
	return p
}

func nilErr(t testing.TB, err error) {
	if err != nil {
		t.Helper()
		t.Fatalf("err: %v", err)
	}
}

func eq[T comparable](t testing.TB, a, b T) {
	if a != b {
		t.Helper()
		t.Fatalf("%v != %v", a, b)
	}
}

func difft(t testing.TB, a, b any) {
	changes, err := diff.Diff(a, b)
	nilErr(t, err)

	if len(changes) == 0 {
		return
	}

	t.Helper()

	for _, c := range changes {
		path := strings.Join(c.Path, ".")
		switch c.Type {
		case diff.DELETE:
			t.Logf("(del) %s\t%q", path, c.From)
		case diff.UPDATE:
			t.Logf("(chg) %s\t%q -> %q", path, c.From, c.To)
		case diff.CREATE:
			t.Logf("(add) %s\t%q", path, c.To)
		}
	}

	t.FailNow()
}
