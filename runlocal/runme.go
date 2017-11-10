package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/radovskyb/watcher"
)

var (
	doAutoGen = true
	wwwDir    = ""
)

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sURL := r.URL.Path[1:]

	if sURL == "" {
		//workspace is the entry point,
		sURL = "runlocal/index.html"
	}

	serveFile(w, r, sURL)
}

func replaceInline(dir, fname, s string) string {
	//serve require.js and app.js locally so we can manipulate them

	//use local require.js
	s = strings.Replace(s, "//chilipeppr.com/js/require.js", "/runlocal/require.js", 1)
	//use local app.js
	s = strings.Replace(s, "'//i2dcui.appspot.com/js/app'", "'//localhost/runlocal/app'", 1)
	//remove slingshot to allow local files
	s = strings.Replace(s, "//i2dcui.appspot.com/slingshot?url=", "", 1)
	// disable cprequire_test
	s = strings.Replace(s, "cprequire.apply(this, arguments);", "", 1)

	// unminify
	s = strings.Replace(s, "//code.jquery.com/jquery-2.1.0.min", "//code.jquery.com/jquery-2.1.0", 1)

	if len(dir) > 0 && fname == "widget.html" {
		//use nested root widgetes inside workspace
		s = strings.Replace(s, "=\"widget.", "=\""+dir+"widget.", -1)
	}

	return s
}

func fileToString(name string) (string, error) {
	if strings.HasPrefix(name, "/") {
		return "", errors.New("inv file name")
	}
	filedata, err := ioutil.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(filedata), nil
}

func serveFile(w http.ResponseWriter, r *http.Request, name string) {

	s, err := fileToString(wwwDir + name)
	d, err2 := os.Stat(wwwDir + name)
	if err != nil || err2 != nil {
		log.Println("404:", name)
		if strings.Contains(name, ".js") {
			http.Error(w, "debugger;", 404)
		} else {
			http.Error(w, "404 page not found", 404)
		}
		return
	}

	dir, fname := filepath.Split(name)
	//dir = dir[3:]

	filedata := []byte(replaceInline(dir, fname, s))

	http.ServeContent(w, r, fname, d.ModTime(), bytes.NewReader(filedata))
}

func watchFiles(w *watcher.Watcher) {
	for {
		select {
		case event := <-w.Event:
			if event.FileInfo == nil {
				continue
			}
			if strings.Contains(event.Path, "auto-generated") {
				continue
			}
			if strings.Contains(event.Path, ".git") {
				continue
			}

			fmt.Println("AutogenEvent:", event) // Print the event's info.
			doAutoGen = true

		case err := <-w.Error:
			log.Fatalln(err)
		case <-w.Closed:
			return
		}
	}
}

func autoGenTimer() {
	for {
		if doAutoGen {
			doAutoGen = false
			DoAutoGen("../")
		}
		time.Sleep(time.Millisecond * 500)
	}
}

func DoAutoGen(dirScan string) {
	if dirScan == ".git" {
		return
	}
	dirOrg, _ := os.Getwd()
	os.Chdir(dirScan)

	gen := []string{"workspace", "widget"}
	for _, name := range gen {
		s, err := fileToString(name + ".html")
		if err != nil {
			continue
		}

		log.Println("fileToString:", name)

		now := time.Now().UTC()
		dateS := strconv.Itoa(now.Year()) + "-" + strconv.Itoa(int(now.Month())) + "-" + strconv.Itoa(now.Day())
		dateS += " " + strconv.Itoa(now.Hour()) + ":" + strconv.Itoa(now.Minute())

		s = InilineFileRegex(`<link rel="stylesheet" type="text/css" href="([^"]*)">`, s, "<style type='text/css'>", "</style>")
		s = InilineFileRegex(`<script type='text/javascript' src="([^"]*)"></script>`, s, "<script type='text/javascript'>", "</script>")
		//log.Println(name, m)

		s = strings.Replace(s, "<!--(auto-fill by runme.js-->", name, 1)
		s = strings.Replace(s, "$$$widgetVersion$$$", dateS, 1)

		name = "auto-generated-" + name + ".html"
		err = ioutil.WriteFile(name, []byte(s), 0644)
		log.Println("generated:", name, err)

		//InvalidateGetURLCache(name)

		files, _ := ioutil.ReadDir("./")
		for _, f := range files {
			if f.IsDir() {
				DoAutoGen(f.Name())
			}
		}
	}

	os.Stat("workspace.html")

	os.Chdir(dirOrg)
}

func InvalidateGetURLCache(name string) {
	r, _ := regexp.Compile(`Push  URL: git@([^:]*):([^.]*)`)
	out, _ := exec.Command("git", "remote", "show", "origin").Output()

	m := r.FindAllStringSubmatch(string(out), -1)
	if len(m) <= 0 || len(m[0]) < 3 {
		return
	}
	sHost := m[0][1]
	sRepo := m[0][2]

	//log.Println("!!!!!!!!!!!!!!!", sHost, sRepo)

	if sHost == "github.com" {
		sURL := "http://i2dcui.appspot.com/geturl?forcerefresh=true&url=http://raw.githubusercontent.com/" + sRepo + "/master/" + name
		r, _ := http.Get(sURL)
		if r != nil {
			log.Println("Invalidated:", r.StatusCode, r.ContentLength, sURL)
			r.Body.Close()
		}
		sURL = "http://i2dcui.appspot.com/geturl?forcerefresh=true&url=https://raw.githubusercontent.com/" + sRepo + "/master/" + name
		r, _ = http.Get(sURL)
		if r != nil {
			log.Println("Invalidated:", r.StatusCode, r.ContentLength, sURL)
			r.Body.Close()
		}
	}

	//http://i2dcui.appspot.com/geturl?forcerefresh=true&url=http://raw.githubusercontent.com/ChristophPech/workspace-grbl1/master/auto-generated-workspace.html

	return
}

func InilineFileRegex(exp, s, pre, suf string) string {
	r, _ := regexp.Compile(exp)

	s = r.ReplaceAllStringFunc(s, func(s string) string {
		sub := r.FindStringSubmatch(s)
		sFName := sub[1]

		sFile, err := fileToString(sFName)
		if err == nil {
			log.Println("inlining:", sFName)
			return pre + sFile + suf
		}

		return s
	})
	return s
}

func main() {
	wwwDir, _ = os.Getwd()
	wwwDir += "/../"

	w := watcher.New()
	//w.FilterOps(watcher.Rename)
	go watchFiles(w)
	w.AddRecursive("../")
	go w.Start(time.Millisecond * 100)

	go autoGenTimer()

	http.HandleFunc("/", handler)
	http.ListenAndServe(":80", nil)
}
