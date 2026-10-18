// linktest times the agent legs of a relay in isolation (diagnostics for the
// Go hub evaluation): download from one agent to nowhere, upload generated
// bytes to another, and both at once. Reads agent tokens from a LANHub
// database copy with its APP_KEY; never prints them.
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

func decrypt(key []byte, payload string) string {
	raw, _ := base64.StdEncoding.DecodeString(payload)
	var p struct{ IV, Value, MAC string }
	json.Unmarshal(raw, &p)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(p.IV + p.Value))
	if hex.EncodeToString(m.Sum(nil)) != p.MAC {
		panic("bad mac")
	}
	iv, _ := base64.StdEncoding.DecodeString(p.IV)
	ct, _ := base64.StdEncoding.DecodeString(p.Value)
	b, _ := aes.NewCipher(key)
	cipher.NewCBCDecrypter(b, iv).CryptBlocks(ct, ct)
	return string(ct[:len(ct)-int(ct[len(ct)-1])])
}

type agent struct{ base, token string }

func load(db *sql.DB, key []byte, id int) agent {
	var host, tok string
	var port int
	db.QueryRow(`SELECT host, port, agent_token FROM machines WHERE id = ?`, id).Scan(&host, &port, &tok)
	return agent{fmt.Sprintf("http://%s:%d", host, port), decrypt(key, tok)}
}

func download(a agent, path string) (int64, time.Duration) {
	t := time.Now()
	req, _ := http.NewRequest("GET", a.base+"/api/download?"+url.Values{"path": {path}}.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return n, time.Since(t)
}

func upload(a agent, dir, name string, size int64) time.Duration {
	t := time.Now()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, _ := mw.CreateFormFile("file", name)
		buf := bytes.Repeat([]byte{0x5a}, 1<<20)
		for left := size; left > 0; left -= int64(len(buf)) {
			if left < int64(len(buf)) {
				buf = buf[:left]
			}
			if _, err := part.Write(buf); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		mw.Close()
		pw.Close()
	}()
	req, _ := http.NewRequest("POST", a.base+"/api/upload?"+url.Values{"path": {dir}}.Encode(), pr)
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return time.Since(t)
}

func main() {
	dbPath := flag.String("db", "", "database copy")
	env := flag.String("env", "", "hub.env with APP_KEY")
	src := flag.Int("src", 5, "source machine id")
	dst := flag.Int("dst", 7, "destination machine id")
	path := flag.String("path", "", "file to download from src")
	dir := flag.String("dir", "/tmp/lanhub-linktest", "folder on dst")
	size := flag.Int64("mb", 300, "MiB to upload")
	flag.Parse()
	envRaw, _ := os.ReadFile(*env)
	var key []byte
	for _, l := range strings.Split(string(envRaw), "\n") {
		if strings.HasPrefix(l, "APP_KEY=base64:") {
			key, _ = base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(l), "APP_KEY=base64:"))
		}
	}
	db, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro")
	if err != nil {
		panic(err)
	}
	s, d := load(db, key, *src), load(db, key, *dst)
	mib := func(n int64, t time.Duration) string { return fmt.Sprintf("%.1f MiB/s", float64(n)/1048576/t.Seconds()) }
	n, t := download(s, *path)
	fmt.Printf("download only : %d MiB in %.1fs = %s\n", n>>20, t.Seconds(), mib(n, t))
	t = upload(d, *dir, "linktest-a.bin", *size<<20)
	fmt.Printf("upload only   : %d MiB in %.1fs = %s\n", *size, t.Seconds(), mib(*size<<20, t))
	var wg sync.WaitGroup
	var dn int64
	var dt, ut time.Duration
	wg.Add(2)
	go func() { defer wg.Done(); dn, dt = download(s, *path) }()
	go func() { defer wg.Done(); ut = upload(d, *dir, "linktest-b.bin", *size<<20) }()
	wg.Wait()
	fmt.Printf("both at once  : download %s, upload %s\n", mib(dn, dt), mib(*size<<20, ut))
}
