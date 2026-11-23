package handler_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/handler/mocks"
	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

var info = handler.ReportInfo{Site: "Home", Version: "dev", Location: time.UTC}

func sampleReport() report.Report {
	d := func(s string) time.Time {
		t, _ := time.Parse(time.DateOnly, s)
		return t
	}
	return report.Build(report.Input{
		Range: report.Range{From: d("2026-06-01"), To: d("2026-06-01")},
		Days: []report.Day{{Date: d("2026-06-01"), Values: map[string]float64{
			report.KeyConsumption: 4000, report.KeyImport: 1000, report.KeyExport: 0,
		}}},
	})
}

func TestReport(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockReportService(t)
	h, err := handler.NewReport(svc, info, clocktest.New(time.Unix(0, 0)), zerolog.Nop())
	require.NoError(t, err)
	q := service.ReportQuery{From: "2026-06-01", To: "2026-06-01", Compare: "none"}
	svc.EXPECT().Build(mock.Anything, q).Return(sampleReport(), nil).Times(3)

	rec := get(h, "/api/v1/report?from=2026-06-01&to=2026-06-01&compare=none")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"self_sufficiency_pct":75`)

	rec = get(h, "/api/v1/report?from=2026-06-01&to=2026-06-01&compare=none&format=csv")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/csv; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, `attachment; filename="pvmon-report-2026-06-01_2026-06-01.csv"`,
		rec.Header().Get("Content-Disposition"))
	assert.Contains(t, rec.Body.String(), "2026-06-01,2026-06-01,2026-06-01,false,1,1,4.00")

	rec = get(h, "/api/v1/report?from=2026-06-01&to=2026-06-01&compare=none&format=pdf&lang=de")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
	assert.True(t, strings.HasPrefix(rec.Body.String(), "%PDF-"))

	rec = get(h, "/api/v1/report?format=xml&lang=fr")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"path":"format"`)
	assert.Contains(t, rec.Body.String(), `"path":"lang"`)

	_, err = handler.NewReport(nil, handler.ReportInfo{}, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestReportErrors(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockReportService(t)
	h, err := handler.NewReport(svc, info, clocktest.New(time.Unix(0, 0)), zerolog.Nop())
	require.NoError(t, err)
	svc.EXPECT().Build(mock.Anything, service.ReportQuery{From: "x"}).
		Return(report.Report{}, fmt.Errorf("parse: %w", report.ErrInvalidRange))
	svc.EXPECT().Build(mock.Anything, service.ReportQuery{From: "y"}).
		Return(report.Report{}, errors.New("disk"))
	rec := get(h, "/api/v1/report?from=x")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"invalid_range"`)
	rec = get(h, "/api/v1/report?from=y")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
