package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/report/csv"
	"github.com/dombyte/pvmon/internal/report/pdf"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/util"
)

// ReportService builds reports.
type ReportService interface {
	Build(ctx context.Context, q service.ReportQuery) (report.Report, error)
}

// ReportInfo is what the PDF header shows besides the report.
type ReportInfo struct {
	Site, Version string
	Location      *time.Location
}

// Report serves GET /api/v1/report (spec 9.1, 12).
type Report struct {
	svc   ReportService
	info  ReportInfo
	clock util.Clock
	log   zerolog.Logger
}

// NewReport builds the handler.
func NewReport(svc ReportService, info ReportInfo, clock util.Clock, log zerolog.Logger,
) (*Report, error) {
	if err := util.RequireAll("handler", util.D("service", svc), util.D("clock", clock),
		util.D("location", info.Location)); err != nil {
		return nil, err
	}
	return &Report{svc: svc, info: info, clock: clock, log: log}, nil
}

func (h *Report) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if details := reportParams(format, q.Get("lang")); len(details) > 0 {
		fail(w, apiError{
			http.StatusBadRequest, "invalid_param", "invalid query parameters", details,
		}, h.log)
		return
	}
	rep, err := h.svc.Build(r.Context(), service.ReportQuery{
		From: q.Get("from"), To: q.Get("to"), Granularity: q.Get("granularity"),
		Compare: q.Get("compare"),
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	switch format {
	case "csv":
		h.download(w, rep, "csv", "text/csv; charset=utf-8", func(b *bytes.Buffer) error {
			return csv.Write(b, rep)
		})
	case "pdf":
		meta := pdf.Meta{
			Site: h.info.Site, Version: h.info.Version, Lang: q.Get("lang"),
			Generated: h.clock.Now().In(h.info.Location),
		}
		h.download(w, rep, "pdf", "application/pdf", func(b *bytes.Buffer) error {
			return pdf.Write(b, rep, meta)
		})
	default:
		writeJSON(w, http.StatusOK, service.NewReportModel(rep), h.log)
	}
}

// reportParams checks format and lang.
func reportParams(format, lang string) []errorDetail {
	var out []errorDetail
	switch format {
	case "", "json", "csv", "pdf":
	default:
		out = append(out, errorDetail{Path: "format", Message: "want json, csv or pdf"})
	}
	switch lang {
	case "", "de", "en":
	default:
		out = append(out, errorDetail{Path: "lang", Message: "want de or en"})
	}
	return out
}

// download renders a file into memory first, so a failure still answers with the error
// shape instead of a cut file.
func (h *Report) download(w http.ResponseWriter, rep report.Report, ext, contentType string,
	render func(*bytes.Buffer) error,
) {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`,
		fileName(rep, ext)))
	if _, err := w.Write(buf.Bytes()); err != nil {
		h.log.Debug().Err(err).Msg("write report")
	}
}

// fileName is pvmon-report-<from>_<to>.<ext>.
func fileName(rep report.Report, ext string) string {
	return fmt.Sprintf("pvmon-report-%s_%s.%s", rep.Range.From.Format("2006-01-02"),
		rep.Range.To.Format("2006-01-02"), ext)
}

func (h *Report) writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, report.ErrInvalidRange) {
		fail(w, apiError{http.StatusBadRequest, "invalid_range", err.Error(), nil}, h.log)
		return
	}
	h.log.Error().Err(err).Msg("report")
	fail(w, apiError{
		http.StatusInternalServerError, "internal", "the report is not available",
		nil,
	}, h.log)
}
