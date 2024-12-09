package taglib_test

import (
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "eg.flac")
	_, err := taglib.ReadTags(path)
	eq(t, err, taglib.ErrInvalidFile)
}

func TestClear(t *testing.T) {
	t.Parallel()

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

			if len(got) > 0 {
				t.Fatalf("exp empty, got %v", got)
			}
		})
	}
}

func TestReadWrite(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	testTags := []map[string][]string{
		{
			"ONE":  {"one", "two", "three", "four"},
			"FIVE": {"six", "seven"},
			"NINE": {"nine"},
		},
		{
			"ARTIST":     {"Example A", "Hello, 世界"},
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
		{
			"ARTIST": {"Hello, 世界", "界世"},
		},
		{
			"ARTIST":      {"Hello, 世界", "界世"},
			"ALBUM":       {longString},
			"ALBUMARTIST": {longString, longString},
			"OTHER":       {strings.Repeat(longString, 2)},
		},
	}

	for _, path := range paths {
		for i, tags := range testTags {
			t.Run(fmt.Sprintf("%s_tags_%d", filepath.Base(path), i), func(t *testing.T) {
				err := taglib.WriteTags(path, tags)
				nilErr(t, err)

				got, err := taglib.ReadTags(path)
				nilErr(t, err)

				if !maps.EqualFunc(got, tags, slices.Equal) {
					t.Fatalf("%v != %v", got, tags)
				}
			})
		}
	}
}

func TestConcurrent(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	path := tmpf(t, egFLAC, "eg.flac")

	properties, err := taglib.ReadProperties(path)
	nilErr(t, err)

	eq(t, 1*time.Second, properties.Length)
	eq(t, 1460, properties.Bitrate)
	eq(t, 48_000, properties.SampleRate)
	eq(t, 2, properties.Channels)
}

func TestMultiOpen(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

	for range b.N {
		err := taglib.WriteTags(path, bigTags)
		nilErr(b, err)
	}
}

func BenchmarkRead(b *testing.B) {
	path := tmpf(b, egFLAC, "eg.flac")
	err := taglib.WriteTags(path, bigTags)
	nilErr(b, err)
	b.ResetTimer()

	for range b.N {
		_, err := taglib.ReadTags(path)
		nilErr(b, err)
	}
}

func checkMem(t testing.TB) {
	stop := make(chan struct{})
	t.Cleanup(func() {
		stop <- struct{}{}
	})

	go func() {
		ticker := time.Tick(100 * time.Millisecond)

		for {
			select {
			case <-stop:
				return

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

var bigTags = map[string][]string{
	"ALBUM":                      {"New Raceion"},
	"ALBUMARTIST":                {"Alan Vega"},
	"ALBUMARTIST_CREDIT":         {"Alan Vega"},
	"ALBUMARTISTS":               {"Alan Vega"},
	"ALBUMARTISTS_CREDIT":        {"Alan Vega"},
	"ARTIST":                     {"Alan Vega"},
	"ARTIST_CREDIT":              {"Alan Vega"},
	"ARTISTS":                    {"Alan Vega"},
	"ARTISTS_CREDIT":             {"Alan Vega"},
	"DATE":                       {"1993-04-02"},
	"DISCNUMBER":                 {"1"},
	"GENRE":                      {"electronic"},
	"GENRES":                     {"electronic", "industrial", "experimental", "proto-punk", "rock", "rockabilly"},
	"LABEL":                      {"GM Editions"},
	"MEDIA":                      {"Digital Media"},
	"MUSICBRAINZ_ALBUMARTISTID":  {"dd720ac8-1c68-4484-abb7-0546413a55e3"},
	"MUSICBRAINZ_ALBUMID":        {"c56a5905-2b3a-46f5-82c7-ce8eed01f876"},
	"MUSICBRAINZ_ARTISTID":       {"dd720ac8-1c68-4484-abb7-0546413a55e3"},
	"MUSICBRAINZ_RELEASEGROUPID": {"373dcce2-63c4-3e8a-9c2c-bc58ec1bbbf3"},
	"MUSICBRAINZ_TRACKID":        {"2f1c8b43-7b4e-4bc8-aacf-760e5fb747a0"},
	"ORIGINALDATE":               {"1993-04-02"},
	"REPLAYGAIN_ALBUM_GAIN":      {"-4.58 dB"},
	"REPLAYGAIN_ALBUM_PEAK":      {"0.977692"},
	"REPLAYGAIN_TRACK_GAIN":      {"-5.29 dB"},
	"REPLAYGAIN_TRACK_PEAK":      {"0.977661"},
	"TITLE":                      {"Christ Dice"},
	"TRACKNUMBER":                {"2"},
	"UPC":                        {"3760271710486"},
}

var longString = strings.Repeat("E", 65536)
