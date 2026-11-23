package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/util"
)

// SampleSource reads raw polls of one device (collector.Fetcher).
type SampleSource interface {
	FetchAll(ctx context.Context) (profile.Sample, error)
}

// DumpOptions controls a dump.
type DumpOptions struct {
	Count    int
	Interval time.Duration
}

// DumpJob bundles the dependencies of a dump.
type DumpJob struct {
	Source  SampleSource
	Profile *profile.Profile
	Clock   util.Clock
	Log     zerolog.Logger
}

var macAddress = regexp.MustCompile(`(?i)\b([0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b`)

// Dump records opts.Count polls of a device and writes them as a profile.Fixture. Failed
// polls are recorded with their error. MAC addresses are removed.
func Dump(ctx context.Context, job DumpJob, opts DumpOptions, w io.Writer) error {
	src, p, clock, log := job.Source, job.Profile, job.Clock, job.Log
	fx := profile.Fixture{
		Profile: p.ID, Version: p.Version, Source: "dump",
		Interval: opts.Interval.String(), Created: clock.Now().UTC(),
	}
	for i := range opts.Count {
		if i > 0 {
			if err := util.Sleep(ctx, clock, opts.Interval); err != nil {
				return fmt.Errorf("maintenance: dump interrupted: %w", err)
			}
		}
		s := poll(ctx, src)
		log.Info().Int("sample", i+1).Int("of", opts.Count).Str("err", s.Err).Msg("dump")
		fx.Samples = append(fx.Samples, s)
	}
	redact(&fx)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(fx); err != nil {
		return fmt.Errorf("maintenance: write dump: %w", err)
	}
	return nil
}

func poll(ctx context.Context, src SampleSource) profile.Sample {
	s, err := src.FetchAll(ctx)
	if err != nil {
		return profile.Sample{At: s.At, Err: err.Error()}
	}
	return s
}

// redact removes every MAC address from the JSON bodies of all samples.
func redact(fx *profile.Fixture) {
	for i := range fx.Samples {
		for id, body := range fx.Samples[i].JSON {
			fx.Samples[i].JSON[id] = json.RawMessage(
				macAddress.ReplaceAllString(string(body), "00:00:00:00:00:00"))
		}
	}
}
