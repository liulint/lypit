package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Media struct {
	Kind     string  `json:"kind"`
	Source   string  `json:"source"`
	SHA256   string  `json:"sha256,omitempty"`
	Bytes    int64   `json:"bytes,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Duration float64 `json:"duration_seconds,omitempty"`
	FPS      float64 `json:"fps,omitempty"`
}
type Content struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *URLValue `json:"image_url,omitempty"`
	VideoURL *URLValue `json:"video_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}
type URLValue struct {
	URL string `json:"url"`
}
type Request struct {
	Model      string    `json:"model"`
	Content    []Content `json:"content"`
	Resolution string    `json:"resolution"`
	Ratio      string    `json:"ratio"`
	Duration   int       `json:"duration"`
	Audio      bool      `json:"generate_audio"`
}

// Public media is fetched anonymously, on a separate transport from Ark credentials.
// Resolve and validate every connection (including redirects), pinning the dialed IP.
func publicClient() *http.Client {
	t := &http.Transport{TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("无效媒体地址")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("无法解析公网媒体地址")
		}
		if len(ips) == 0 {
			return nil, errors.New("公网媒体地址无解析结果")
		}
		for _, addr := range ips {
			if !isPublic(addr.IP) {
				return nil, errors.New("媒体地址必须是公网地址")
			}
		}
		var last error
		for _, addr := range ips {
			c, e := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	return &http.Client{Transport: t, Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("媒体重定向过多")
		}
		return publicURL(req.URL.String())
	}}
}
func isPublic(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		_, n, _ := net.ParseCIDR(block)
		if n.Contains(ip) {
			return false
		}
	}
	return true
}
func publicURL(s string) error {
	u, e := url.Parse(s)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return errors.New("媒体必须是无用户凭据的 HTTP(S) 公网 URL")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return errors.New("不接受本地媒体地址")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !isPublic(ip) {
		return errors.New("不接受内网媒体地址")
	}
	return nil
}
func dimensionsValid(w, h int) bool {
	r := float64(w) / float64(h)
	return w >= 300 && w <= 6000 && h >= 300 && h <= 6000 && r >= 0.4 && r <= 2.5
}

type probeResult struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Rate      string `json:"avg_frame_rate"`
		Duration  string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Name     string `json:"format_name"`
	} `json:"format"`
}

func probe(ctx context.Context, path string) (probeResult, error) {
	var p probeResult
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// No remote protocols: ffprobe only sees the bounded local copy.
	b, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		return p, errors.New("ffprobe 无法核实素材；请让 AI 检查素材并按需安装 FFmpeg")
	}
	if json.Unmarshal(b, &p) != nil {
		return p, errors.New("无法读取 ffprobe 元数据")
	}
	return p, nil
}
func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func prepare(ctx context.Context, o Options, client *http.Client) (Request, []Media, []string, bool, error) {
	r := Request{Model: o.Model, Resolution: o.Resolution, Ratio: o.Ratio, Duration: o.Duration, Audio: o.Audio, Content: []Content{{Type: "text", Text: o.Prompt}}}
	media := []Media{}
	warnings := []string{}
	valid := true
	for _, path := range o.Images {
		abs, e := filepath.Abs(path)
		if e != nil {
			return r, nil, nil, false, e
		}
		f, e := os.Open(abs)
		if e != nil {
			return r, nil, nil, false, errors.New("无法读取参考图片")
		}
		b, e := io.ReadAll(io.LimitReader(f, 30_000_000))
		f.Close()
		if e != nil || len(b) >= 30_000_000 {
			return r, nil, nil, false, errors.New("参考图片须小于 30 MB")
		}
		cfg, format, e := image.DecodeConfig(bytes.NewReader(b))
		mime := "image/" + format
		w, h := cfg.Width, cfg.Height
		if e != nil {
			ext := strings.ToLower(filepath.Ext(abs))
			formats := map[string]string{".webp": "webp", ".bmp": "bmp", ".tif": "tiff", ".tiff": "tiff", ".heic": "heic", ".heif": "heif"}
			if formats[ext] == "" {
				return r, nil, nil, false, errors.New("图片格式无效；支持 JPEG、PNG、GIF、WebP、BMP、TIFF、HEIC、HEIF")
			}
			// Probe the same bytes that are hashed and submitted, even if the source changes.
			p, err := probeImageBytes(ctx, b)
			if err != nil || len(p.Streams) == 0 {
				return r, nil, nil, false, errors.New("无法核实图片格式或尺寸，其他图片格式需要 ffprobe")
			}
			codec := p.Streams[0].CodecName
			expected := map[string]string{"webp": "webp", "bmp": "bmp", "tiff": "tiff", "heic": "hevc", "heif": "hevc"}
			if codec != expected[formats[ext]] {
				return r, nil, nil, false, errors.New("图片内容与格式不符或此 HEIF 编码不受支持")
			}
			w, h = p.Streams[0].Width, p.Streams[0].Height
			mime = "image/" + formats[ext]
		}
		if !dimensionsValid(w, h) {
			return r, nil, nil, false, errors.New("参考图片宽高须为 300–6000，宽高比须为 0.4–2.5")
		}
		media = append(media, Media{Kind: "image", Source: abs, SHA256: hashBytes(b), Bytes: int64(len(b)), Width: w, Height: h})
		r.Content = append(r.Content, Content{Type: "image_url", Role: "reference_image", ImageURL: &URLValue{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)}})
	}
	var total float64
	for _, raw := range o.Videos {
		if err := publicURL(raw); err != nil {
			return r, nil, nil, false, err
		}
		m, err := inspectVideo(ctx, raw, client)
		if err != nil {
			valid = false
			warnings = append(warnings, fmt.Sprintf("参考视频 %d 无法核实：%s；仅可查看不完整报价，不可提交", len(media)+1, err))
			m = Media{Kind: "video", Source: raw}
		}
		media = append(media, m)
		total += m.Duration
		r.Content = append(r.Content, Content{Type: "video_url", Role: "reference_video", VideoURL: &URLValue{URL: raw}})
	}
	if total > 15.000001 {
		return r, nil, nil, false, errors.New("参考视频总时长不能超过 15 秒")
	}
	body, _ := json.Marshal(r)
	if len(body) > 64_000_000 {
		return r, nil, nil, false, errors.New("编码后请求体超过 64 MB，请减少或压缩参考图片")
	}
	return r, media, warnings, valid, nil
}

func inspectVideo(ctx context.Context, raw string, client *http.Client) (Media, error) {
	m := Media{Kind: "video", Source: raw}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	resp, err := client.Do(req)
	if err != nil {
		return m, errors.New("无法匿名读取公网视频")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return m, fmt.Errorf("读取素材 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 200_000_000 {
		return m, errors.New("视频超过 200 MB")
	}
	f, err := os.CreateTemp("", "hypit-seedance-video-*.media")
	if err != nil {
		return m, errors.New("无法创建素材校验临时文件")
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 200_000_001))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return m, errors.New("读取视频不完整")
	}
	if n > 200_000_000 {
		return m, errors.New("视频超过 200 MB")
	}
	p, err := probe(ctx, f.Name())
	if err != nil {
		return m, err
	}
	if !strings.Contains(p.Format.Name, "mov") && !strings.Contains(p.Format.Name, "mp4") {
		return m, errors.New("参考视频须为 MP4/MOV")
	}
	videoCount := 0
	for _, s := range p.Streams {
		if s.CodecType == "audio" && s.CodecName != "aac" && s.CodecName != "mp3" && !strings.HasPrefix(s.CodecName, "pcm_") {
			return m, errors.New("视频音轨编码须为 AAC、MP3 或 MOV 中的 PCM")
		}
		if s.CodecType != "video" {
			continue
		}
		videoCount++
		if s.CodecName != "h264" && s.CodecName != "hevc" {
			return m, errors.New("参考视频编码须为 H.264 或 HEVC")
		}
		m.Width, m.Height = s.Width, s.Height
		parts := strings.Split(s.Rate, "/")
		if len(parts) == 2 {
			num, _ := strconv.ParseFloat(parts[0], 64)
			den, _ := strconv.ParseFloat(parts[1], 64)
			if den > 0 {
				m.FPS = num / den
			}
		}
		m.Duration, _ = strconv.ParseFloat(s.Duration, 64)
	}
	if m.Duration <= 0 {
		m.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	}
	if videoCount != 1 || !dimensionsValid(m.Width, m.Height) || m.Width*m.Height < 407696 || m.Width*m.Height > 8295044 {
		return m, errors.New("参考视频尺寸、比例或视频流数量不符合官方限制")
	}
	if math.IsNaN(m.Duration) || math.IsInf(m.Duration, 0) || math.IsNaN(m.FPS) || math.IsInf(m.FPS, 0) || m.Duration < 2 || m.Duration > 15 || m.FPS < 24 || m.FPS > 60 {
		return m, errors.New("参考视频须为 2–15 秒、24–60 fps")
	}
	m.Bytes = n
	m.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return m, nil
}

func probeImageBytes(ctx context.Context, b []byte) (probeResult, error) {
	f, err := os.CreateTemp("", "hypit-seedance-image-*.media")
	if err != nil {
		return probeResult{}, err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	ce := f.Close()
	if err != nil {
		return probeResult{}, err
	}
	if ce != nil {
		return probeResult{}, ce
	}
	return probe(ctx, f.Name())
}
