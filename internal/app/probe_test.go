package app_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/simulate"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
)

// busSpy records the transport each probe got from the running collectors.
type busSpy struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (b *busSpy) factory(s *site.Site, d *site.Device, bus collector.Transport,
	clock util.Clock, log zerolog.Logger,
) (collector.Source, func(), error) {
	b.mu.Lock()
	b.seen[d.ID] = bus != nil
	b.mu.Unlock()
	return simulate.Probe(time.UTC)(s, d, bus, clock, log)
}

func (b *busSpy) had(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seen[id]
}

func TestConfigBatchAndProbe(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	spy := &busSpy{seen: map[string]bool{}}
	h.opts.Probe = spy.factory
	stop := runServe(t, h)
	c := newAPIClient(t, h.ln)
	c.setup(h.out)

	draft := `{"kind":"device","id":"ev","body":{"address":"http://10.0.0.32",` +
		`"profile":"shelly/pro-3em"}}`
	status, body := c.do(http.MethodPost, "/api/v1/config/probe",
		`{"objects":[`+draft+`],"device":"ev"}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"profile":"shelly/pro-3em"`)
	assert.Contains(t, body, `"name":"total"`)
	assert.NotContains(t, body, `"error"`)
	assert.False(t, spy.had("ev"), "no collector runs on the new address")
	assert.EqualValues(t, 0, c.siteField("config_rev"), "a probe stores nothing")

	status, body = c.do(http.MethodPost, "/api/v1/config/probe", `{"device":"meter"}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.True(t, spy.had("meter"), "read through the running collector's transport")
	status, _ = c.do(http.MethodPost, "/api/v1/config/probe", `{"device":"ghost"}`)
	assert.Equal(t, http.StatusNotFound, status)
	status, body = c.do(http.MethodPost, "/api/v1/config/probe",
		`{"objects":[{"kind":"device","id":"ev","body":{"profile":"nope/x"}}],"device":"ev"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, status, body)

	status, body = c.do(http.MethodPut, "/api/v1/config", `{"objects":[`+draft+`,`+
		`{"kind":"meter","id":"wallbox","body":{"device":"ev","source":"total",`+
		`"role":"consumer","category":"wallbox"}}]}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"config_rev":1`)
	assert.EqualValues(t, 1, c.siteField("config_rev"), "one revision for the batch")

	status, body = c.do(http.MethodPut, "/api/v1/config",
		`{"objects":[{"kind":"meter","id":"wallbox","body":null},`+
			`{"kind":"meter","id":"pv","body":{"device":"ghost","source":"pv"}}]}`)
	assert.Equal(t, http.StatusUnprocessableEntity, status, body)
	status, body = c.do(http.MethodPut, "/api/v1/config",
		`{"objects":[{"kind":"meter","id":"grid","body":null}]}`)
	assert.Equal(t, http.StatusConflict, status, body)
	status, body = c.do(http.MethodPut, "/api/v1/config",
		`{"objects":[{"kind":"meter","id":"wallbox","body":null},`+
			`{"kind":"device","id":"ev","body":null}]}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.EqualValues(t, 2, c.siteField("config_rev"))
	stop()
}
