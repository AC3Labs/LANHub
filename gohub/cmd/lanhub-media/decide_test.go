package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func stream(kind, codec, pix string) probeStream {
	return probeStream{CodecType: kind, CodecName: codec, PixFmt: pix}
}

func TestDecidePicksTheCheapestWayToPlay(t *testing.T) {
	cases := []struct {
		name, container, file string
		streams                []probeStream
		want                   string
	}{
		{"h264 mp4", "mov,mp4,m4a,3gp,3g2,mj2", "a.mp4", []probeStream{stream("video", "h264", "yuv420p"), stream("audio", "aac", "")}, "direct"},
		{"vp9 webm", "matroska,webm", "a.webm", []probeStream{stream("video", "vp9", "yuv420p"), stream("audio", "opus", "")}, "direct"},
		{"h264 mkv", "matroska,webm", "a.mkv", []probeStream{stream("video", "h264", "yuv420p"), stream("audio", "aac", "")}, "remux"},
		{"h264 + ac3 mkv", "matroska,webm", "a.mkv", []probeStream{stream("video", "h264", "yuv420p"), stream("audio", "ac3", "")}, "audio"},
		{"h264 avi + mp3", "avi", "a.avi", []probeStream{stream("video", "h264", "yuv420p"), stream("audio", "mp3", "")}, "remux"},
		{"vp9 mkv + vorbis", "matroska,webm", "a.mkv", []probeStream{stream("video", "vp9", "yuv420p"), stream("audio", "vorbis", "")}, "audio"},
		{"hevc mp4", "mov,mp4,m4a,3gp,3g2,mj2", "a.mp4", []probeStream{stream("video", "hevc", "yuv420p"), stream("audio", "aac", "")}, "transcode"},
		{"xvid avi", "avi", "a.avi", []probeStream{stream("video", "mpeg4", "yuv420p"), stream("audio", "mp3", "")}, "transcode"},
		{"h264 10-bit", "matroska,webm", "a.mkv", []probeStream{stream("video", "h264", "yuv420p10le")}, "transcode"},
		{"wmv", "asf", "a.wmv", []probeStream{stream("video", "wmv2", "yuv420p"), stream("audio", "wmav2", "")}, "transcode"},
		{"music with cover art", "mp3", "a.mp3", []probeStream{{CodecType: "video", CodecName: "mjpeg", Disp: struct {
			AttachedPic int `json:"attached_pic"`
		}{1}}, stream("audio", "mp3", "")}, "audio_only"},
		{"nothing", "data", "a.bin", nil, "unplayable"},
	}
	for _, c := range cases {
		var p probeResult
		p.Streams = c.streams
		p.Format.FormatName = c.container
		p.Format.Duration = "12.5"
		got := decide(p, c.file)
		if got.Mode != c.want {
			t.Errorf("%s: got %s (%s), want %s", c.name, got.Mode, got.Reason, c.want)
		}
		if c.want != "unplayable" && got.Duration != 12.5 {
			t.Errorf("%s: duration %v", c.name, got.Duration)
		}
	}
}

func TestFfmpegArgsUseTheGPUOnlyForConversion(t *testing.T) {
	s := &server{cfg: config{maxHeight: 720, threads: 4, vaapiDevice: "/dev/dri/renderD128"}}
	tr := strings.Join(s.ffmpegArgs("transcode", "http://hub/x", 30, "ac3"), " ")
	if !strings.Contains(tr, "h264_vaapi") || !strings.Contains(tr, "-ss 30.00") || !strings.Contains(tr, "min(ih\\,720)") {
		t.Fatalf("transcode args: %s", tr)
	}
	rm := strings.Join(s.ffmpegArgs("remux", "http://hub/x", 0, "aac"), " ")
	if strings.Contains(rm, "vaapi") || !strings.Contains(rm, "-c:v copy") || !strings.Contains(rm, "-c:a copy") || strings.Contains(rm, "-ss") {
		t.Fatalf("remux args: %s", rm)
	}
	s.cfg.vaapiDevice = ""
	sw := strings.Join(s.ffmpegArgs("transcode", "http://hub/x", 0, "aac"), " ")
	if !strings.Contains(sw, "libx264") || !strings.Contains(sw, "-threads 4") {
		t.Fatalf("software args: %s", sw)
	}
	for _, a := range [][]string{s.ffmpegArgs("audio", "x", 0, "dts")} {
		j := strings.Join(a, " ")
		if !strings.Contains(j, "-c:v copy") || !strings.Contains(j, "-c:a aac") {
			t.Fatalf("audio args: %s", j)
		}
	}
}

func TestStreamWaitsBrieflyForASlotThenRefuses(t *testing.T) {
	s := &server{cfg: config{secret: "0123456789abcdef"}, transcodes: make(chan struct{}, 1), remuxes: make(chan struct{}, 1)}
	s.transcodes <- struct{}{} // the one slot is taken
	go func() { time.Sleep(300 * time.Millisecond); <-s.transcodes }()
	// Freed within the wait: the request would go ahead (it then fails on the
	// bogus source, which is fine for this test).
	start := time.Now()
	req := httptest.NewRequest("GET", "/stream?mode=transcode&source=http://127.0.0.1:1/x", nil)
	rec := httptest.NewRecorder()
	s.stream(rec, req)
	if rec.Code == http.StatusServiceUnavailable || time.Since(start) < 250*time.Millisecond {
		t.Fatalf("should have waited for the slot, got %d after %s", rec.Code, time.Since(start))
	}
}
