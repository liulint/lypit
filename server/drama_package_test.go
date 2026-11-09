package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func packageUpload(t *testing.T, s *Studio, id string, b []byte, status int) DramaWork {
	t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/drama/works/import?operation_id="+id, bytes.NewReader(b))
	r.Header.Set("X-Lypit-Client", "studio")
	r.Header.Set("Content-Type", "application/zip")
	w := httptest.NewRecorder()
	s.router().ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	var work DramaWork
	if status == 200 {
		if e := json.Unmarshal(w.Body.Bytes(), &work); e != nil {
			t.Fatal(e)
		}
	}
	return work
}
func packageBytes(t *testing.T, p dramaPackage, extra map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, e := z.Create(dramaPackageManifest)
	if e != nil {
		t.Fatal(e)
	}
	f.Write(jsonBytes(p))
	for name, data := range extra {
		f, e = z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		f.Write(data)
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestDramaPackageRoundTripAndIdempotency(t *testing.T) {
	source := testStudio(t)
	w := dramaTestWork(t, source)
	d := dramaTestSave(t, source, w, "script", dramaMock("script", 5)+"\n人工保留内容。")
	var original DramaMedia
	json.Unmarshal(request(t, source, "POST", "/api/drama/works/"+w.ID+"/media/import", map[string]any{"entry": "固定角色", "name": "猫.png", "data": base64.StdEncoding.EncodeToString(mockPNG("cat", false, false))}, 200), &original)
	request(t, source, "POST", "/api/drama/works/"+w.ID+"/media/"+original.ID+"/approve", map[string]string{"category": "character"}, 200)
	b := request(t, source, "GET", "/api/drama/works/"+w.ID+"/package", nil, 200)
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range z.File {
		if f.Name == dramaPackageManifest {
			r, _ := f.Open()
			var p dramaPackage
			json.NewDecoder(r).Decode(&p)
			r.Close()
			if p.Work.ID != "" || p.Work.Profile.Vendor != "" || p.Work.Context != nil || p.Media[0].Media.TaskID != "" || p.Media[0].Media.File != "" {
				t.Fatal("private execution data exported")
			}
		}
	}
	dest := testStudio(t)
	existing := dramaTestWork(t, dest)
	id := uid()
	imported := packageUpload(t, dest, id, b, 200)
	if imported.ID != id || imported.ID == w.ID || imported.Title != w.Title || imported.SeriesID != "" {
		t.Fatal(imported)
	}
	got, e := dest.dramaDoc(id, "script")
	if e != nil || got.Text != d.Text || got.State != "draft" || got.AcceptedHash != "" {
		t.Fatalf("document mismatch: %+v %v", got, e)
	}
	media, e := dest.dramaMedias(id)
	if e != nil || len(media) != 1 {
		t.Fatalf("media: %v %v", media, e)
	}
	m := media[0]
	if m.ID == original.ID || m.Approved || m.TaskID != "" || !m.Current || m.Path != original.Path || m.Hash != original.Hash || m.Category != "character" {
		t.Fatal(m)
	}
	if e = dest.verifyDramaMedia(m); e != nil {
		t.Fatal(e)
	}
	if _, e = dest.dramaWork(existing.ID); e != nil {
		t.Fatal("existing work lost")
	}
	packageUpload(t, dest, id, b, 200)
	tasks, e := dest.store.tasks(id)
	if e != nil || len(tasks) != 0 {
		t.Fatal("import created execution tasks")
	}
	media, _ = dest.dramaMedias(id)
	if len(media) != 1 {
		t.Fatal("retry duplicated media")
	}
	packageUpload(t, dest, id, append(b, 0), 409)
	// A separate user action creates an independent copy.
	packageUpload(t, dest, uid(), b, 200)
}
func TestDramaPackageRejectsInvalidArchivesWithoutWrites(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	p := dramaPackage{Format: "lypit-work", Version: 1, Work: portableDramaWork(w), Documents: map[string]string{"script": "documents/script.md"}}
	for _, tc := range []struct {
		name string
		edit func(*dramaPackage, map[string][]byte)
	}{
		{"traversal", func(p *dramaPackage, f map[string][]byte) { f["../escape"] = []byte("bad") }},
		{"missing", func(p *dramaPackage, f map[string][]byte) { delete(f, "documents/script.md") }},
		{"unknown version", func(p *dramaPackage, f map[string][]byte) { p.Version = 99 }},
		{"hash mismatch", func(p *dramaPackage, f map[string][]byte) {
			p.Media = []dramaPackageMedia{{Blob: "media/a.png", Media: DramaMedia{Kind: "image", Path: "制作成果/a.png", Hash: "wrong"}}}
			f["media/a.png"] = []byte("bad")
		}},
		{"unlisted", func(p *dramaPackage, f map[string][]byte) { f["extra.txt"] = []byte("bad") }},
		{"unknown kind", func(p *dramaPackage, f map[string][]byte) { p.Documents["unknown"] = "unknown.md" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := p
			copy.Documents = map[string]string{"script": "documents/script.md"}
			f := map[string][]byte{"documents/script.md": []byte("正文")}
			tc.edit(&copy, f)
			id := uid()
			packageUpload(t, s, id, packageBytes(t, copy, f), 400)
			if _, e := s.dramaWork(id); e == nil {
				t.Fatal("partial work published")
			}
			if name, _ := s.dramaFile(id, dramaPaths["script"]); name != "" {
				if _, e := os.Stat(name); !os.IsNotExist(e) {
					t.Fatal("invalid archive wrote files")
				}
			}
		})
	}
	packageUpload(t, s, uid(), []byte("invalid ZIP"), 400)
}
func TestDramaPackageRejectsDuplicateAndOversizeEntries(t *testing.T) {
	s := testStudio(t)
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	z.Create("duplicate")
	z.Create("duplicate")
	z.Close()
	packageUpload(t, s, uid(), b.Bytes(), 400)
	// Exercise the size gate without allocating a huge ZIP bomb.
	zr := &zip.Reader{File: []*zip.File{{FileHeader: zip.FileHeader{Name: "large.mp4", UncompressedSize64: 101 << 20}}}}
	if _, e := s.importDramaPackage(uid(), "hash", zr); e == nil {
		t.Fatal("oversize entry accepted")
	}
}

func TestDramaPackageRollsBackFilesAndDatabaseOnInstallFailure(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	p := dramaPackage{Format: "lypit-work", Version: 1, Work: portableDramaWork(w), Documents: map[string]string{"script": "documents/script.md"}}
	data := mockPNG("rollback", false, false)
	p.Media = []dramaPackageMedia{{Blob: "media/cat.png", Media: DramaMedia{Kind: "image", Entry: "猫", Path: "制作成果/cat.png", Hash: digest(data)}}}
	// Fail after the Markdown and media files have been installed.
	if _, e := s.store.db.Exec("CREATE TRIGGER reject_import_media BEFORE INSERT ON drama_media BEGIN SELECT RAISE(ABORT,'test installation failure'); END"); e != nil {
		t.Fatal(e)
	}
	id := uid()
	packageUpload(t, s, id, packageBytes(t, p, map[string][]byte{"documents/script.md": []byte("人工正文"), "media/cat.png": data}), 400)
	if _, e := s.dramaWork(id); e == nil {
		t.Fatal("failed import published a work")
	}
	name, _ := s.dramaFile(id, dramaPaths["script"])
	if _, e := os.Stat(name); !os.IsNotExist(e) {
		t.Fatal("failed import retained document")
	}
	entries, e := os.ReadDir(s.media(id))
	if e != nil || len(entries) != 0 {
		t.Fatalf("failed import retained media: %v %v", entries, e)
	}
	var count int
	if e = s.store.db.QueryRow("SELECT count(*) FROM projects WHERE id=?", id).Scan(&count); e != nil || count != 0 {
		t.Fatal("shell not rolled back", e)
	}
}
