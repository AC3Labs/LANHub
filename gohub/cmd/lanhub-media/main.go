// Command lanhub-media is LANHub Media: a small service, run in its own
// container next to the hub, that makes any video playable in a browser.
// It does the least work each file needs:
//
//	direct     the browser can already play it: nothing to do (the hub
//	           streams it as is)
//	remux      the codecs are fine but the container isn't (MKV, AVI, TS…):
//	           repackage without re-encoding, almost no CPU
//	audio      only the soundtrack is unplayable (AC-3, DTS…): copy the
//	           picture, convert the sound
//	transcode  convert the picture to H.264, on the GPU when one is available,
//	           at most 720p, one at a time, at low priority
//
// Output is fragmented MP4, which a browser plays as it arrives. Each stream
// stops the moment the viewer goes away. The hub is the only caller: every
// request needs the shared secret, and the source is a short-lived signed
// link back to the hub, which checks access and fetches from the agent.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const version = "0.1.0.0"

type config struct {
	listen        string
	secret        string
	maxTranscodes int
	maxRemux      int
	maxHeight     int
	vaapiDevice   string // "" = software encoding
	threads       int
}

// waitForSecret reads the secret the hub writes on its first start into a
// volume both containers share. On a new install the two start together, so
// give the hub a moment to create it.
func waitForSecret(path string, wait time.Duration) string {
	deadline := time.Now().Add(wait)
	for {
		if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
			return strings.TrimSpace(string(b))
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(time.Second)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

type server struct {
	cfg        config
	transcodes chan struct{}
	remuxes    chan struct{}
	active     atomic.Int32
}

func main() {
	cfg := config{
		listen:        getenv("MEDIA_LISTEN", ":8960"),
		secret:        os.Getenv("MEDIA_SECRET"),
		maxTranscodes: atoi(os.Getenv("MEDIA_MAX_TRANSCODES"), 1),
		maxRemux:      atoi(os.Getenv("MEDIA_MAX_REMUX"), 3),
		maxHeight:     atoi(os.Getenv("MEDIA_MAX_HEIGHT"), 720),
		threads:       atoi(os.Getenv("MEDIA_THREADS"), 4),
	}
	if cfg.secret == "" && os.Getenv("MEDIA_SECRET_FILE") != "" {
		cfg.secret = waitForSecret(os.Getenv("MEDIA_SECRET_FILE"), 2*time.Minute)
	}
	if len(cfg.secret) < 16 {
		log.Fatal("MEDIA_SECRET must be set (16+ characters) and shared with the hub, or MEDIA_SECRET_FILE must point at the file the hub creates")
	}
	cfg.vaapiDevice = detectVAAPI(getenv("MEDIA_HWACCEL", "auto"))
	s := &server{cfg: cfg, transcodes: make(chan struct{}, cfg.maxTranscodes), remuxes: make(chan struct{}, cfg.maxRemux)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /probe", s.auth(s.probe))
	mux.HandleFunc("GET /stream", s.auth(s.stream))
	srv := &http.Server{Addr: cfg.listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	hw := "software (libx264)"
	if cfg.vaapiDevice != "" {
		hw = "VAAPI on " + cfg.vaapiDevice
	}
	log.Printf("LANHub Media %s on %s: video encoding by %s, up to %d conversion(s) and %d repackaging(s) at once, max %dp",
		version, cfg.listen, hw, cfg.maxTranscodes, cfg.maxRemux, cfg.maxHeight)
	log.Fatal(srv.ListenAndServe())
}

// detectVAAPI checks that the GPU can really encode H.264 by encoding one
// tiny frame; a device node alone isn't proof.
func detectVAAPI(mode string) string {
	if mode == "none" {
		return ""
	}
	dev := "/dev/dri/renderD128"
	if _, err := os.Stat(dev); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-v", "error", "-vaapi_device", dev,
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=1", "-frames:v", "1",
		"-vf", "format=nv12,hwupload", "-c:v", "h264_vaapi", "-f", "null", "-")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("VAAPI not usable (%v): %s", err, strings.TrimSpace(string(out)))
		return ""
	}
	return dev
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	want := "Bearer " + s.cfg.secret
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-version").Output()
	first, _, _ := strings.Cut(string(out), "\n")
	writeJSON(w, 200, map[string]any{"ok": true, "version": version, "ffmpeg": first,
		"hwaccel": s.cfg.vaapiDevice != "", "active": s.active.Load()})
}

// ---- probing and deciding ----

type probeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Profile   string `json:"profile"`
	PixFmt    string `json:"pix_fmt"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Channels  int    `json:"channels"`
	Disp      struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

// Plan is what the hub and the player need to know.
type Plan struct {
	Mode      string  `json:"mode"` // direct | remux | audio | transcode | audio_only | unplayable
	Reason    string  `json:"reason"`
	Duration  float64 `json:"duration"`
	Container string  `json:"container"`
	Video     string  `json:"video,omitempty"`
	Audio     string  `json:"audio,omitempty"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	HW        bool    `json:"hw"`
}

// What current Chrome, Edge and Firefox all play natively.
var (
	browserVideo = map[string]bool{"h264": true, "vp8": true, "vp9": true, "av1": true}
	browserAudio = map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true}
	mp4Audio     = map[string]bool{"aac": true, "mp3": true, "opus": true, "flac": true} // can be copied into MP4
)

func decide(p probeResult, filename string) Plan {
	plan := Plan{Container: p.Format.FormatName}
	plan.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	var v, a *probeStream
	for i := range p.Streams {
		st := &p.Streams[i]
		if st.CodecType == "video" && st.Disp.AttachedPic == 0 && v == nil {
			v = st
		}
		if st.CodecType == "audio" && a == nil {
			a = st
		}
	}
	if a != nil {
		plan.Audio = a.CodecName
	}
	if v == nil {
		if a == nil {
			plan.Mode, plan.Reason = "unplayable", "No video or audio found in this file."
			return plan
		}
		plan.Mode, plan.Reason = "audio_only", "Audio only."
		return plan
	}
	plan.Video, plan.Width, plan.Height = v.CodecName, v.Width, v.Height
	videoOK := browserVideo[v.CodecName] && (v.CodecName != "h264" || (v.PixFmt == "yuv420p" || v.PixFmt == "yuvj420p"))
	audioOK := a == nil || browserAudio[a.CodecName]
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(filename), "."))
	mp4ish := strings.Contains(plan.Container, "mp4") && (ext == "mp4" || ext == "m4v" || ext == "mov")
	webm := ext == "webm" && (v.CodecName == "vp8" || v.CodecName == "vp9" || v.CodecName == "av1")
	switch {
	case videoOK && audioOK && (mp4ish || webm) && (a == nil || mp4Audio[a.CodecName] || webm):
		plan.Mode, plan.Reason = "direct", "The browser plays this as it is."
	case videoOK && audioOK && (a == nil || mp4Audio[a.CodecName]):
		plan.Mode, plan.Reason = "remux", "Repackaged for the browser (no re-encoding)."
	case videoOK:
		plan.Mode, plan.Reason = "audio", "Picture copied; sound converted to AAC."
	default:
		plan.Mode, plan.Reason = "transcode", fmt.Sprintf("Converted from %s to H.264 for the browser.", strings.ToUpper(v.CodecName))
	}
	return plan
}

func (s *server) probe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source   string `json:"source"`
		Filename string `json:"filename"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil || req.Source == "" {
		writeJSON(w, 400, map[string]string{"message": "source is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams",
		"-analyzeduration", "5000000", "-probesize", "10000000", req.Source)
	out, err := cmd.Output()
	if err != nil {
		msg := "The file couldn't be read."
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			msg += " " + lastLine(string(ee.Stderr))
		}
		writeJSON(w, 422, map[string]string{"message": msg})
		return
	}
	var p probeResult
	if json.Unmarshal(out, &p) != nil {
		writeJSON(w, 422, map[string]string{"message": "The file's details couldn't be read."})
		return
	}
	plan := decide(p, req.Filename)
	plan.HW = s.cfg.vaapiDevice != ""
	writeJSON(w, 200, plan)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// ---- streaming ----

// ffmpegArgs builds the command for one mode, starting at start seconds.
func (s *server) ffmpegArgs(mode, source string, start float64, audioCodec string) []string {
	args := []string{"-hide_banner", "-v", "error", "-nostdin"}
	hw := mode == "transcode" && s.cfg.vaapiDevice != ""
	if hw {
		args = append(args, "-vaapi_device", s.cfg.vaapiDevice)
	}
	if start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', 2, 64))
	}
	args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-i", source,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn")
	h := s.cfg.maxHeight
	switch mode {
	case "transcode":
		if hw {
			args = append(args, "-vf", fmt.Sprintf("format=nv12,hwupload,scale_vaapi=w=-2:h='min(ih\\,%d)'", h),
				"-c:v", "h264_vaapi", "-qp", "24")
		} else {
			args = append(args, "-vf", fmt.Sprintf("scale=-2:'min(ih,%d)',format=yuv420p", h),
				"-c:v", "libx264", "-preset", "veryfast", "-crf", "24", "-threads", strconv.Itoa(s.cfg.threads))
		}
		args = append(args, "-g", "60")
	default:
		args = append(args, "-c:v", "copy")
	}
	if mode == "remux" && mp4Audio[audioCodec] {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "160k", "-ac", "2")
	}
	return append(args, "-f", "mp4", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "1000000", "pipe:1")
}

func (s *server) stream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode, source := q.Get("mode"), q.Get("source")
	start, _ := strconv.ParseFloat(q.Get("start"), 64)
	if source == "" || (mode != "remux" && mode != "audio" && mode != "transcode") || start < 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	slots := s.remuxes
	if mode == "transcode" {
		slots = s.transcodes
	}
	// Wait a moment for a free slot: when a viewer seeks, the new stream
	// arrives just before the old one has finished stopping.
	wait := time.NewTimer(5 * time.Second)
	defer wait.Stop()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-wait.C:
		log.Printf("%s refused: already at the limit", mode)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "LANHub Media is already converting as many videos as it's allowed to. Close another preview, or try again in a moment."})
		return
	case <-r.Context().Done():
		return
	}
	s.active.Add(1)
	defer s.active.Add(-1)

	// The viewer closing the preview cancels the request, which kills ffmpeg.
	args := s.ffmpegArgs(mode, source, start, q.Get("audio"))
	cmd := exec.CommandContext(r.Context(), "nice", append([]string{"-n", "10", "ffmpeg"}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		http.Error(w, "couldn't start ffmpeg: "+err.Error(), 500)
		return
	}
	began := time.Now()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	n, copyErr := io.Copy(flushWriter{w}, stdout)
	waitErr := cmd.Wait()
	switch {
	case r.Context().Err() != nil:
		log.Printf("%s from %.0fs stopped by the viewer after %s (%d MB)", mode, start, time.Since(began).Round(time.Second), n>>20)
	case waitErr != nil || copyErr != nil:
		log.Printf("%s from %.0fs failed after %s: %v %v %s", mode, start, time.Since(began).Round(time.Second), waitErr, copyErr, strings.TrimSpace(stderr.String()))
	default:
		log.Printf("%s from %.0fs finished in %s (%d MB)", mode, start, time.Since(began).Round(time.Second), n>>20)
	}
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	l.w.Write(p)
	return len(p), nil
}
