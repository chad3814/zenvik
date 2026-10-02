package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
)

func newInfoCmd() *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "info <path>",
		Short: "List the titles on a Blu-ray or DVD disc image or folder",
		Long: `List the titles (playlists or DVD titles) on a Blu-ray or DVD disc image or folder, ranked so
the likely main feature (★) comes first. Duplicates, encrypted titles and
multi-angle titles are noted; very short or unusable titles are hidden
unless --all is given. DVD titles that zenvik can't rip yet are hidden too;
--all shows them with the reason.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{fmt.Errorf("info needs exactly one path, got %d", len(args))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSettings(config.Flags{})
			if err != nil {
				return err
			}
			d, err := zenvik.Open(cmd.Context(), args[0], zenvik.WithMinDuration(s.MinDuration))
			if err != nil {
				return err
			}
			defer d.Close()
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), d)
			}
			writeTable(cmd.OutOrStdout(), cmd.ErrOrStderr(), d, all)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "also list filtered titles and why they were filtered")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the disc as JSON (always includes every title)")
	return cmd
}

func writeTable(out, errw io.Writer, d *zenvik.Disc, all bool) {
	name := d.Label
	if d.Meta != nil && d.Meta.Title != "" {
		name = d.Meta.Title
	}
	fmt.Fprintf(out, "%s  (%s: %s)\n\n", name, kindLabel(d), d.Path)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "\tID\tDURATION\tSIZE\tCH\tVIDEO\tAUDIO\tSUBS\tNOTES")
	hidden := 0
	for _, t := range d.Titles {
		if t.Rank.Filtered && !all {
			hidden++
			continue
		}
		mark := ""
		if t.Rank.IsMain {
			mark = "★"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", mark, t.ID, formatDuration(t.Duration),
			formatSize(t.Size), len(t.Chapters), videoSummary(t), audioLanguages(t), subtitleLanguages(t), notes(t))
	}
	tw.Flush()
	if hidden > 0 {
		fmt.Fprintf(out, "\n%d filtered title(s) hidden; use --all to show them.\n", hidden)
	}
	m := d.Main()
	switch {
	case m == nil:
		fmt.Fprintln(out, "\nNo title qualifies as the main feature.")
	case m.Rank.Ambiguous:
		fmt.Fprintf(errw, "warning: the main title is a close call (%s)\n", closeSecond(m))
	}
}

func notes(t *zenvik.Title) string {
	var n []string
	if t.Rank.DuplicateOf != "" {
		n = append(n, "duplicate of "+t.Rank.DuplicateOf)
	}
	if t.Encrypted {
		n = append(n, "encrypted")
	}
	if t.Angles > 1 {
		n = append(n, fmt.Sprintf("%d angles", t.Angles))
	}
	if t.Rank.Ambiguous {
		n = append(n, "ambiguous")
	}
	if k := len(t.SkippedCells); k > 0 {
		n = append(n, fmt.Sprintf("skipped %d short cell(s)", k))
	}
	if t.Rank.Filtered {
		var why []string
		for _, r := range t.Rank.Reasons {
			if r != "encrypted" {
				why = append(why, r)
			}
		}
		if len(why) > 0 {
			n = append(n, strings.Join(why, "; "))
		}
	}
	return strings.Join(n, ", ")
}

func closeSecond(t *zenvik.Title) string {
	for _, r := range t.Rank.Reasons {
		if strings.HasPrefix(r, "close second") {
			return r
		}
	}
	return "another title scored almost as high"
}

type jsonDisc struct {
	Path     string      `json:"path"`
	Kind     string      `json:"kind"`
	Format   string      `json:"format"`
	Label    string      `json:"label"`
	Title    string      `json:"title,omitempty"`
	Language string      `json:"language,omitempty"`
	Main     string      `json:"main,omitempty"`
	Titles   []jsonTitle `json:"titles"`
}

type jsonTitle struct {
	ID              string         `json:"id"`
	DurationSeconds float64        `json:"duration_seconds"`
	SizeBytes       int64          `json:"size_bytes"`
	Chapters        []float64      `json:"chapters"` // start times in seconds
	Angles          int            `json:"angles"`
	Encrypted       bool           `json:"encrypted"`
	Unsupported     string         `json:"unsupported,omitempty"`
	RipMethod       string         `json:"rip_method,omitempty"`
	SkippedCells    []jsonSkipped  `json:"skipped_cells,omitempty"`
	Video           []jsonVideo    `json:"video"`
	Audio           []jsonAudio    `json:"audio"`
	Subtitles       []jsonSubtitle `json:"subtitles"`
	Rank            jsonRank       `json:"rank"`
}

type jsonSkipped struct {
	Cell            int     `json:"cell"`
	DurationSeconds float64 `json:"duration_seconds"`
	FirstSector     uint32  `json:"first_sector"`
	LastSector      uint32  `json:"last_sector"`
}

type jsonVideo struct {
	PID          uint16 `json:"pid"`
	Codec        string `json:"codec"`
	Format       string `json:"format"`
	FrameRate    string `json:"frame_rate"`
	DynamicRange string `json:"dynamic_range"`
	AspectRatio  string `json:"aspect_ratio,omitempty"`
}

type jsonAudio struct {
	PID         uint16 `json:"pid"`
	Codec       string `json:"codec"`
	Language    string `json:"language"`
	Channels    string `json:"channels"`
	SampleRate  string `json:"sample_rate"`
	Description string `json:"description,omitempty"`
}

type jsonSubtitle struct {
	PID         uint16 `json:"pid"`
	Codec       string `json:"codec"`
	Language    string `json:"language"`
	Description string `json:"description,omitempty"`
}

type jsonRank struct {
	Score       float64  `json:"score"`
	Main        bool     `json:"main"`
	Ambiguous   bool     `json:"ambiguous"`
	Filtered    bool     `json:"filtered"`
	DuplicateOf string   `json:"duplicate_of,omitempty"`
	Reasons     []string `json:"reasons"`
}

// kindName is the machine-readable source kind for JSON output.
func kindName(k zenvik.SourceKind) string {
	switch k {
	case zenvik.ISO:
		return "iso"
	case zenvik.BDMVDir:
		return "bdmv"
	case zenvik.VideoTSDir:
		return "video_ts"
	}
	return "unknown"
}

// formatName is the machine-readable disc format for JSON output.
func formatName(f zenvik.Format) string {
	switch f {
	case zenvik.Bluray:
		return "bluray"
	case zenvik.DVD:
		return "dvd"
	}
	return "unknown"
}

func writeJSON(w io.Writer, d *zenvik.Disc) error {
	out := jsonDisc{Path: d.Path, Kind: kindName(d.Kind), Format: formatName(d.Format), Label: d.Label, Titles: make([]jsonTitle, 0, len(d.Titles))}
	if d.Meta != nil {
		out.Title, out.Language = d.Meta.Title, d.Meta.Language
	}
	if m := d.Main(); m != nil {
		out.Main = m.ID
	}
	for _, t := range d.Titles {
		out.Titles = append(out.Titles, toJSONTitle(t))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func toJSONTitle(t *zenvik.Title) jsonTitle {
	jt := jsonTitle{
		ID:              t.ID,
		DurationSeconds: t.Duration.Seconds(),
		SizeBytes:       t.Size,
		Chapters:        make([]float64, 0, len(t.Chapters)),
		Angles:          t.Angles,
		Encrypted:       t.Encrypted,
		Unsupported:     t.Unsupported,
		RipMethod:       t.RipMethod,
		Video:           make([]jsonVideo, 0, len(t.Video)),
		Audio:           make([]jsonAudio, 0, len(t.Audio)),
		Subtitles:       make([]jsonSubtitle, 0, len(t.Subtitles)),
		Rank: jsonRank{
			Score:       t.Rank.Score,
			Main:        t.Rank.IsMain,
			Ambiguous:   t.Rank.Ambiguous,
			Filtered:    t.Rank.Filtered,
			DuplicateOf: t.Rank.DuplicateOf,
			Reasons:     append([]string{}, t.Rank.Reasons...),
		},
	}
	for _, sc := range t.SkippedCells {
		jt.SkippedCells = append(jt.SkippedCells, jsonSkipped{Cell: sc.Cell, DurationSeconds: sc.Duration.Seconds(),
			FirstSector: sc.FirstSector, LastSector: sc.LastSector})
	}
	for _, c := range t.Chapters {
		jt.Chapters = append(jt.Chapters, c.Start.Seconds())
	}
	for _, v := range t.Video {
		jt.Video = append(jt.Video, jsonVideo{PID: v.PID, Codec: codecName(v.Codec), Format: v.Format.String(),
			FrameRate: v.FrameRate.String(), DynamicRange: v.DynamicRange.String(), AspectRatio: v.AspectRatio})
	}
	for _, a := range t.Audio {
		jt.Audio = append(jt.Audio, jsonAudio{PID: a.PID, Codec: codecName(a.Codec), Language: a.Language,
			Channels: a.Channels.String(), SampleRate: a.SampleRate.String(), Description: a.Description})
	}
	for _, s := range t.Subtitles {
		jt.Subtitles = append(jt.Subtitles, jsonSubtitle{PID: s.PID, Codec: codecName(s.Codec), Language: s.Language, Description: s.Description})
	}
	return jt
}
