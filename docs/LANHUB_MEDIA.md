# LANHub Media

A separate container, shipped with LANHub, that lets the Explorer preview
nearly any video in the browser. Built 2026-10-18 for the Go hub
(`gohub/cmd/lanhub-media`); running on the Docker Server (192.168.0.70:8960).

## How a video is played

The hub asks LANHub Media to inspect the file (ffprobe), which picks the
cheapest mode:

| Mode | When | Cost |
|---|---|---|
| direct | The browser plays it as is (H.264/VP8/VP9/AV1 with AAC/MP3/Opus/Vorbis/FLAC in MP4/MOV/WebM) | none: the hub streams the file |
| remux | Codecs fine, container not (H.264 in MKV/AVI/TS/FLV…) | repackaging only, ~5% CPU |
| audio | Picture fine, sound not (AC-3, DTS, WMA, PCM…) | sound converted to AAC, ~10% |
| transcode | Picture not playable (HEVC, MPEG-1/2, Xvid/DivX, WMV, VC-1, ProRes, MJPEG, FLV1, Theora, 10-bit H.264…) | H.264 on the GPU (VAAPI) when available, else libx264 `veryfast`; at most 720p |

Output is fragmented MP4 streamed as it's made. The LANHub player shows the
file's real length and seeks by restarting the stream at the new point.

## Load limits

- One full conversion and three repackagings at once (`MEDIA_MAX_TRANSCODES`,
  `MEDIA_MAX_REMUX`); a new request waits up to 5 s for a slot, then gets a
  "busy" message.
- ffmpeg runs at `nice 10`, at most `MEDIA_THREADS` (4) threads in software
  mode, and is killed the moment the viewer closes the preview or seeks.
- GPU encoding is used only if a real test encode succeeds at start-up.
- Measured on the Docker Server (i7-4800MQ, HD 4600, VAAPI): conversions
  peaked at 16-32% of total CPU; repackaging 5-10%.

## Security

- The browser only talks to the hub. LANHub Media needs `MEDIA_SECRET`
  (shared with the hub) on every request.
- LANHub Media never sees agent tokens. It reads a file through a signed hub
  link (`/media-source`): HMAC over machine + path + expiry, key derived from
  APP_KEY, valid 12 hours, one file only. Access is checked by the hub before
  any link is made.
- Agents before 1.2.0 can't send part of a file, so the hub skips ahead in
  the stream to answer range requests (correct, but jumping near the end of a
  huge file reads everything before it). Agent 1.2.0 answers ranges itself.

## Formats tested (2026-10-18, 16 clips, 720p, all played)

direct: H.264 MP4, H.264 MOV, VP9 WebM, VP8 WebM.
remux: H.264 MKV. audio: H.264 + AC-3 MKV.
transcode: HEVC MP4, Xvid AVI, MPEG-2 MPG, MPEG-1 MPG, WMV2, 10-bit H.264
MKV, ProRes MOV, MJPEG AVI, FLV1, Theora OGV. Seek in a converted HEVC file:
resumes in ~2 s.

The image's ffmpeg (Alpine 3.22, ffmpeg 6.1.2) has 288 video and 210 audio
decoders. Image size 193 MB.

## Configuration

Hub: `MEDIA_URL` (http://192.168.0.70:8960), `MEDIA_SECRET`,
`MEDIA_SOURCE_BASE` (the hub as LANHub Media reaches it; nas wired address
http://192.168.0.48:8952 in the test setup). Without them, previews fall back
to direct play only.

Media container: `MEDIA_SECRET`, optional `MEDIA_MAX_TRANSCODES`,
`MEDIA_MAX_REMUX`, `MEDIA_MAX_HEIGHT`, `MEDIA_THREADS`, `MEDIA_HWACCEL=none`.
Run with `--device /dev/dri --group-add <render gid>` for GPU encoding.

## Licensing

The image contains Alpine's ffmpeg build, which is GPL (it includes x264).
LANHub Media runs ffmpeg as a separate program and doesn't link to it, so
LANHub's own code keeps its license. Distributing the image means also
distributing ffmpeg: include its license text and point to Alpine's source
packages. "VLC" isn't used anywhere.
