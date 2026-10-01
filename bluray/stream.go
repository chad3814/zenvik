package bluray

import "fmt"

// CodingType is a stream_coding_type value.
type CodingType uint8

// Coding types used on Blu-ray discs.
const (
	CodingMPEG1Video     CodingType = 0x01
	CodingMPEG2Video     CodingType = 0x02
	CodingMPEG1Audio     CodingType = 0x03
	CodingMPEG2Audio     CodingType = 0x04
	CodingAVC            CodingType = 0x1B
	CodingMVC            CodingType = 0x20
	CodingHEVC           CodingType = 0x24
	CodingLPCM           CodingType = 0x80
	CodingAC3            CodingType = 0x81
	CodingDTS            CodingType = 0x82
	CodingTrueHD         CodingType = 0x83
	CodingEAC3           CodingType = 0x84
	CodingDTSHDHR        CodingType = 0x85
	CodingDTSHDMA        CodingType = 0x86
	CodingPG             CodingType = 0x90
	CodingIG             CodingType = 0x91
	CodingTextST         CodingType = 0x92
	CodingEAC3Secondary  CodingType = 0xA1
	CodingDTSHDSecondary CodingType = 0xA2
	CodingVC1            CodingType = 0xEA
)

// StreamKind groups coding types by elementary stream category.
type StreamKind uint8

// Stream kinds.
const (
	KindUnknown StreamKind = iota
	KindVideo
	KindAudio
	KindPG // presentation graphics (PGS subtitles)
	KindIG // interactive graphics (menus)
	KindText
)

// Kind returns the stream category of c.
func (c CodingType) Kind() StreamKind {
	switch c {
	case CodingMPEG1Video, CodingMPEG2Video, CodingAVC, CodingMVC, CodingHEVC, CodingVC1:
		return KindVideo
	case CodingMPEG1Audio, CodingMPEG2Audio, CodingLPCM, CodingAC3, CodingDTS, CodingTrueHD,
		CodingEAC3, CodingDTSHDHR, CodingDTSHDMA, CodingEAC3Secondary, CodingDTSHDSecondary:
		return KindAudio
	case CodingPG:
		return KindPG
	case CodingIG:
		return KindIG
	case CodingTextST:
		return KindText
	}
	return KindUnknown
}

var codingNames = map[CodingType]string{
	CodingMPEG1Video: "MPEG-1 Video", CodingMPEG2Video: "MPEG-2 Video",
	CodingMPEG1Audio: "MPEG-1 Audio", CodingMPEG2Audio: "MPEG-2 Audio",
	CodingAVC: "H.264/AVC", CodingMVC: "H.264/MVC", CodingHEVC: "HEVC", CodingVC1: "VC-1",
	CodingLPCM: "LPCM", CodingAC3: "AC-3", CodingDTS: "DTS", CodingTrueHD: "TrueHD",
	CodingEAC3: "E-AC-3", CodingDTSHDHR: "DTS-HD HR", CodingDTSHDMA: "DTS-HD MA",
	CodingEAC3Secondary: "E-AC-3 (secondary)", CodingDTSHDSecondary: "DTS-HD (secondary)",
	CodingPG: "PGS", CodingIG: "IGS", CodingTextST: "Text subtitle",
}

func (c CodingType) String() string {
	if n, ok := codingNames[c]; ok {
		return n
	}
	return fmt.Sprintf("0x%02X", uint8(c))
}

// VideoFormat is the video_format field (resolution and scan type).
type VideoFormat uint8

// FrameRate is the frame_rate field.
type FrameRate uint8

// DynamicRange is the dynamic_range_type field of HEVC streams.
type DynamicRange uint8

// AudioFormat is the audio_presentation_type field.
type AudioFormat uint8

// SampleRate is the sampling_frequency field.
type SampleRate uint8

func enumName[T ~uint8](names map[T]string, v T) string {
	if n, ok := names[v]; ok {
		return n
	}
	return fmt.Sprintf("unknown(%d)", uint8(v))
}

var (
	videoFormatNames = map[VideoFormat]string{
		1: "480i", 2: "576i", 3: "480p", 4: "1080i", 5: "720p", 6: "1080p", 7: "576p", 8: "2160p",
	}
	frameRateNames = map[FrameRate]string{
		1: "23.976", 2: "24", 3: "25", 4: "29.97", 6: "50", 7: "59.94",
	}
	dynamicRangeNames = map[DynamicRange]string{0: "SDR", 1: "HDR10", 2: "Dolby Vision"}
	audioFormatNames  = map[AudioFormat]string{
		1: "mono", 3: "stereo", 6: "multi-channel", 12: "stereo + multi-channel",
	}
	sampleRateNames = map[SampleRate]string{
		1: "48 kHz", 4: "96 kHz", 5: "192 kHz", 12: "48/192 kHz", 14: "48/96 kHz",
	}
)

func (v VideoFormat) String() string  { return enumName(videoFormatNames, v) }
func (v FrameRate) String() string    { return enumName(frameRateNames, v) }
func (v DynamicRange) String() string { return enumName(dynamicRangeNames, v) }
func (v AudioFormat) String() string  { return enumName(audioFormatNames, v) }
func (v SampleRate) String() string   { return enumName(sampleRateNames, v) }

// Stream describes one elementary stream from a playlist STN table or a
// clip's program info. Fields that do not apply to the stream kind are zero.
type Stream struct {
	PID          uint16
	Coding       CodingType
	VideoFormat  VideoFormat
	FrameRate    FrameRate
	DynamicRange DynamicRange
	AudioFormat  AudioFormat
	SampleRate   SampleRate
	Language     string // ISO 639-2 code for audio and subtitle streams
}

// parseAttributes reads a length-prefixed stream attribute block: MPLS
// stream_attributes when clip is false, CLPI StreamCodingInfo when clip is
// true. The layouts differ only for HEVC video, where CLPI has an extra
// byte (aspect ratio and flags) before the dynamic range (libbluray
// mpls_parse.c and clpi_parse.c).
func parseAttributes(r *reader, s *Stream, clip bool) {
	ar := r.sub(int(r.u8()))
	s.Coding = CodingType(ar.u8())
	switch s.Coding.Kind() {
	case KindVideo:
		b := ar.u8()
		s.VideoFormat, s.FrameRate = VideoFormat(b>>4), FrameRate(b&0x0F)
		if s.Coding != CodingHEVC {
			return
		}
		if clip {
			if ar.remaining() < 2 {
				return
			}
			ar.skip(1) // aspect ratio, oc_flag, cr_flag
		}
		if ar.remaining() >= 1 {
			s.DynamicRange = DynamicRange(ar.u8() >> 4)
		}
	case KindAudio:
		b := ar.u8()
		s.AudioFormat, s.SampleRate = AudioFormat(b>>4), SampleRate(b&0x0F)
		s.Language = ar.str(3)
	case KindPG, KindIG:
		s.Language = ar.str(3)
	case KindText:
		ar.skip(1) // character code
		s.Language = ar.str(3)
	}
}
